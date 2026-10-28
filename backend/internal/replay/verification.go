package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/fidibeki/app.fidibeki/backend/internal/abuse"
	"github.com/hibiken/asynq"
)

const TaskVerifyBatch = "replay:verify-batch"

var ErrNoBatchVerificationWork = errors.New("no replay batch verification work")

const uploadCleanupGrace = time.Minute

type BatchVerificationTask struct {
	TenantID   string `json:"tenant_id"`
	BatchID    string `json:"batch_id"`
	Generation int    `json:"generation"`
}

type BatchVerificationWork struct {
	Batch
	PageURL      string
	RRWebVersion string
}

type BatchVerificationStore interface {
	Claim(context.Context, BatchVerificationTask) (BatchVerificationWork, error)
	MarkSafe(context.Context, BatchVerificationWork, string, time.Time) error
	CompleteSafeCleanup(context.Context, BatchVerificationWork, time.Time) error
	MarkRejected(context.Context, BatchVerificationWork, FailureCode, string, time.Time) error
	CompleteRejectedCleanup(context.Context, BatchVerificationWork, time.Time) error
}

type BatchVerificationStorage interface {
	OpenObject(context.Context, string) (io.ReadCloser, error)
	PutObject(context.Context, string, io.Reader, int64, string) error
	RemoveObject(context.Context, string) error
}

type BatchDiagnosticRecorder interface {
	Record(context.Context, abuse.DiagnosticEvent) error
}

type BatchVerificationProcessor struct {
	Store       BatchVerificationStore
	Storage     BatchVerificationStorage
	Diagnostics BatchDiagnosticRecorder
	Reporter    FailureReporter
	Now         func() time.Time
	Logger      *slog.Logger
}

func (processor BatchVerificationProcessor) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		payload, err := decodeBatchVerificationTask(task.Payload())
		if err != nil {
			return fmt.Errorf("decode replay batch verification: %w: %w", err, asynq.SkipRetry)
		}
		work, err := processor.Store.Claim(ctx, payload)
		if errors.Is(err, ErrNoBatchVerificationWork) {
			return nil
		}
		if err != nil {
			return err
		}
		if work.SafetyStatus == BatchSafetySafe {
			stagingKey, err := stagingBatchKey(work.StorageKey)
			if err != nil {
				return err
			}
			if err := processor.Storage.RemoveObject(ctx, stagingKey); err != nil {
				return err
			}
			if processor.now().Before(work.UploadExpiresAt.Add(uploadCleanupGrace)) {
				return nil
			}
			return processor.Store.CompleteSafeCleanup(ctx, work, processor.now())
		}
		if work.SafetyStatus == BatchSafetyRejected {
			if err := processor.Storage.RemoveObject(ctx, work.StorageKey); err != nil {
				return err
			}
			if processor.now().Before(work.UploadExpiresAt.Add(uploadCleanupGrace)) {
				return nil
			}
			return processor.Store.CompleteRejectedCleanup(ctx, work, processor.now())
		}
		reader, err := processor.Storage.OpenObject(ctx, work.StorageKey)
		if err != nil {
			return err
		}
		temporary, stageErr := stageBatchVerificationObject(reader, work.SizeBytes)
		closeErr := reader.Close()
		if closeErr != nil && stageErr == nil {
			stageErr = fmt.Errorf("close replay batch verification stream: %w", closeErr)
		}
		if stageErr != nil {
			return processor.rejectUnsafe(ctx, work, stageErr)
		}
		defer func() {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}()
		_, verifyErr := decodeVerifiedBatch(temporary, work.Batch, work.PageURL, nil)
		if verifyErr != nil {
			return processor.rejectUnsafe(ctx, work, verifyErr)
		}
		if _, err := temporary.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("rewind verified replay batch: %w", err)
		}
		verifiedKey := verifiedBatchKey(
			work.StorageKey, work.Generation, work.VerificationTries,
		)
		if err := processor.Storage.PutObject(
			ctx, verifiedKey, temporary, work.SizeBytes, work.ContentType,
		); err != nil {
			return err
		}
		if err := processor.Store.MarkSafe(ctx, work, verifiedKey, processor.now()); err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			cleanupErr := processor.Storage.RemoveObject(cleanupCtx, verifiedKey)
			cancel()
			return errors.Join(err, cleanupErr)
		}
		if err := processor.Storage.RemoveObject(ctx, work.StorageKey); err != nil {
			return err
		}
		if processor.now().Before(work.UploadExpiresAt.Add(uploadCleanupGrace)) {
			return nil
		}
		return processor.Store.CompleteSafeCleanup(ctx, work, processor.now())
	}
}

func stageBatchVerificationObject(reader io.Reader, declaredSize int64) (*os.File, error) {
	if declaredSize < 1 || declaredSize > maximumBatchSize {
		return nil, unsafeReplayBatch{reason: "size_mismatch"}
	}
	temporary, err := os.CreateTemp("", "fidibeki-replay-verification-*")
	if err != nil {
		return nil, fmt.Errorf("create replay verification file: %w", err)
	}
	written, copyErr := io.Copy(temporary, io.LimitReader(reader, declaredSize+1))
	if copyErr != nil {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return nil, fmt.Errorf("stage replay verification body: %w", copyErr)
	}
	if written != declaredSize {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return nil, unsafeReplayBatch{reason: "size_mismatch"}
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
		return nil, fmt.Errorf("rewind replay verification body: %w", err)
	}
	return temporary, nil
}

func verifiedBatchKey(stagingKey string, generation int, attempt int) string {
	return fmt.Sprintf("%s.verified-%d-%d", stagingKey, generation, attempt)
}

func stagingBatchKey(verifiedKey string) (string, error) {
	marker := strings.LastIndex(verifiedKey, ".verified-")
	if marker < 1 {
		return "", errors.New("verified replay storage key is invalid")
	}
	parts := strings.Split(verifiedKey[marker+len(".verified-"):], "-")
	if len(parts) < 1 || len(parts) > 2 {
		return "", errors.New("verified replay storage key is invalid")
	}
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 1 {
			return "", errors.New("verified replay storage key is invalid")
		}
	}
	return verifiedKey[:marker], nil
}

func (processor BatchVerificationProcessor) rejectUnsafe(
	ctx context.Context,
	work BatchVerificationWork,
	verifyErr error,
) error {
	var unsafe unsafeReplayBatch
	if !errors.As(verifyErr, &unsafe) {
		return verifyErr
	}
	reason := unsafe.reason
	if unsafe.violationCode != "" {
		reason = string(unsafe.violationCode)
	}
	if err := processor.Store.MarkRejected(
		ctx, work, unsafe.code(), reason, processor.now(),
	); err != nil {
		return err
	}
	if unsafe.code() == FailureContractViolation && processor.Reporter != nil {
		processor.Reporter.ReportReplayFailure(ctx, FailureReport{
			Operation: FailureOperationVerifyBatch, Code: unsafe.code(),
			Violation: unsafe.violationCode, RRWebVersion: work.RRWebVersion,
			EventType: unsafe.eventType, IncrementalSource: unsafe.incrementalSource,
		})
	}
	if err := processor.Storage.RemoveObject(ctx, work.StorageKey); err != nil {
		return err
	}
	if !processor.now().Before(work.UploadExpiresAt.Add(uploadCleanupGrace)) {
		if err := processor.Store.CompleteRejectedCleanup(ctx, work, processor.now()); err != nil {
			return err
		}
	}
	if unsafe.code() != FailureContractViolation {
		processor.recordRejected(ctx, work, unsafe.reason)
	}
	return nil
}

func decodeBatchVerificationTask(payload []byte) (BatchVerificationTask, error) {
	var task BatchVerificationTask
	if err := json.Unmarshal(payload, &task); err != nil {
		return BatchVerificationTask{}, err
	}
	if strings.TrimSpace(task.TenantID) == "" || strings.TrimSpace(task.BatchID) == "" ||
		task.Generation < 1 {
		return BatchVerificationTask{}, errors.New("replay batch verification task is invalid")
	}
	return task, nil
}

func (processor BatchVerificationProcessor) recordRejected(
	ctx context.Context,
	work BatchVerificationWork,
	reason string,
) {
	if processor.Diagnostics == nil {
		return
	}
	if err := processor.Diagnostics.Record(ctx, abuse.DiagnosticEvent{
		TenantID: work.TenantID, ProjectID: work.ProjectID,
		Category: abuse.CategoryUploadSafety, Reason: "replay_" + reason,
		OccurredAt: processor.now(),
	}); err != nil {
		processor.logger().WarnContext(ctx, "record rejected replay diagnostic", "error", err)
	}
}

func (processor BatchVerificationProcessor) now() time.Time {
	if processor.Now != nil {
		return processor.Now().UTC()
	}
	return time.Now().UTC()
}

func (processor BatchVerificationProcessor) logger() *slog.Logger {
	if processor.Logger != nil {
		return processor.Logger
	}
	return slog.Default()
}

type unsafeReplayBatch struct {
	reason            string
	failureCode       FailureCode
	violationCode     ViolationCode
	eventType         int
	incrementalSource int
}

func (failure unsafeReplayBatch) Error() string {
	return failure.reason
}

func (failure unsafeReplayBatch) code() FailureCode {
	if failure.failureCode == "" {
		return FailureUnsafePayload
	}
	return failure.failureCode
}

type ViolationCode string

const ViolationMediaFieldsNotSupported ViolationCode = "media_fields_not_supported"

func decodeVerifiedBatch(
	reader io.Reader,
	batch Batch,
	expectedPageURL string,
	consume func([]byte) error,
) (int, error) {
	if batch.SizeBytes < 1 || batch.SizeBytes > maximumBatchSize {
		return 0, unsafeReplayBatch{reason: "size_mismatch"}
	}
	canonicalPageURL, validPageURL := canonicalReplayURL(expectedPageURL)
	if !validPageURL || canonicalPageURL != expectedPageURL {
		return 0, unsafeReplayBatch{reason: "invalid_session_page"}
	}
	body, err := io.ReadAll(io.LimitReader(reader, batch.SizeBytes+1))
	if err != nil {
		return 0, fmt.Errorf("read replay batch %d: %w", batch.Index, err)
	}
	if int64(len(body)) != batch.SizeBytes {
		return 0, unsafeReplayBatch{reason: "size_mismatch"}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return 0, unsafeReplayBatch{reason: "expected_array"}
	}
	count := 0
	for decoder.More() {
		var event json.RawMessage
		if err := decoder.Decode(&event); err != nil {
			return 0, unsafeReplayBatch{reason: "invalid_json"}
		}
		if violation := validateRRWebEvent(event, expectedPageURL); violation != nil {
			return 0, *violation
		}
		if consume != nil {
			if err := consume(event); err != nil {
				return 0, err
			}
		}
		count++
		if count > 50 {
			return 0, unsafeReplayBatch{reason: "event_count_mismatch"}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return 0, unsafeReplayBatch{reason: "invalid_json"}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return 0, unsafeReplayBatch{reason: "trailing_json"}
	}
	if count != batch.EventsCount {
		return 0, unsafeReplayBatch{reason: "event_count_mismatch"}
	}
	return count, nil
}

func validRRWebEvent(raw json.RawMessage, expectedPageURL string) bool {
	return validateRRWebEvent(raw, expectedPageURL) == nil
}

func validateRRWebEvent(raw json.RawMessage, expectedPageURL string) *unsafeReplayBatch {
	if validRRWebEventShape(raw, expectedPageURL) {
		return nil
	}
	if event, ok := decodeReplayObject(raw); ok &&
		hasOnlyKeys(event, "type", "timestamp", "data") &&
		positiveReplayNumber(event["timestamp"]) {
		eventType, typeOK := replayInteger(event["type"])
		data, dataOK := replayObject(event["data"])
		if typeOK && eventType == 3 && dataOK && replayMediaContractDrift(data) {
			return &unsafeReplayBatch{
				reason: "contract_violation", failureCode: FailureContractViolation,
				violationCode: ViolationMediaFieldsNotSupported,
				eventType:     eventType, incrementalSource: 7,
			}
		}
	}
	return &unsafeReplayBatch{reason: "invalid_event", failureCode: FailureUnsafePayload}
}

func validRRWebEventShape(raw json.RawMessage, expectedPageURL string) bool {
	event, ok := decodeReplayObject(raw)
	if !ok || !hasOnlyKeys(event, "type", "timestamp", "data") {
		return false
	}
	eventType, ok := replayInteger(event["type"])
	if !ok || eventType < 0 || eventType > 5 || !positiveReplayNumber(event["timestamp"]) {
		return false
	}
	data, ok := replayObject(event["data"])
	if !ok {
		return false
	}
	switch eventType {
	case 0, 1:
		return len(data) == 0
	case 2:
		if !hasOnlyKeys(data, "node", "initialOffset") {
			return false
		}
		if _, exists := data["node"]; !exists || !validReplaySnapshotNode(data["node"], false) {
			return false
		}
		if offset, exists := data["initialOffset"]; exists &&
			!validReplayNumberObject(offset, "top", "left") {
			return false
		}
		return true
	case 3:
		return validReplayIncrementalData(data)
	case 4:
		return validReplayMetaData(data, expectedPageURL)
	case 5:
		return validReplayCustomData(data, expectedPageURL)
	default:
		return false
	}
}

func replayMediaContractDrift(data map[string]any) bool {
	const mediaSource = 7
	source, ok := replayInteger(data["source"])
	if !ok || source != mediaSource || hasOnlyKeys(
		data, "source", "type", "id", "currentTime", "volume", "muted", "playbackRate", "loop",
	) {
		return false
	}
	known := make(map[string]any, 8)
	knownKeys := map[string]struct{}{}
	for _, key := range []string{
		"source", "type", "id", "currentTime", "volume", "muted", "playbackRate", "loop",
	} {
		knownKeys[key] = struct{}{}
		if value, exists := data[key]; exists {
			known[key] = value
		}
	}
	for key, value := range data {
		if _, exists := knownKeys[key]; exists {
			continue
		}
		switch value.(type) {
		case nil, bool, json.Number:
		default:
			return false
		}
	}
	return validReplayMediaData(known)
}

func decodeReplayObject(raw []byte) (map[string]any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return object, true
}

func replayObject(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok && object != nil
}

func hasOnlyKeys(object map[string]any, keys ...string) bool {
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[key] = struct{}{}
	}
	for key := range object {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func replayNumber(value any) (float64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := number.Float64()
	return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
}

func positiveReplayNumber(value any) bool {
	parsed, ok := replayNumber(value)
	return ok && parsed > 0
}

func replayInteger(value any) (int, bool) {
	parsed, ok := replayNumber(value)
	if !ok || parsed != math.Trunc(parsed) ||
		parsed < float64(math.MinInt) || parsed > float64(math.MaxInt) {
		return 0, false
	}
	return int(parsed), true
}

func replayID(value any, nullable bool) bool {
	if nullable && value == nil {
		return true
	}
	parsed, ok := replayInteger(value)
	return ok && parsed >= 0
}

func replayBoolean(value any) bool {
	_, ok := value.(bool)
	return ok
}

func validReplayNumberObject(value any, keys ...string) bool {
	object, ok := replayObject(value)
	if !ok || !hasOnlyKeys(object, keys...) {
		return false
	}
	for _, key := range keys {
		if candidate, exists := object[key]; exists {
			if _, ok := replayNumber(candidate); !ok {
				return false
			}
		}
	}
	return true
}

func validReplayMetaData(data map[string]any, expectedPageURL string) bool {
	if !hasOnlyKeys(data, "href", "width", "height") {
		return false
	}
	href, ok := data["href"].(string)
	if !ok {
		return false
	}
	canonical, valid := canonicalReplayURL(href)
	if !valid || canonical != href || href != expectedPageURL {
		return false
	}
	for _, key := range []string{"width", "height"} {
		if value, exists := data[key]; exists {
			number, ok := replayNumber(value)
			if !ok || number < 0 {
				return false
			}
		}
	}
	return true
}

func validReplayCustomData(data map[string]any, expectedPageURL string) bool {
	if !hasOnlyKeys(data, "tag", "payload") || data["tag"] != "navigation" {
		return false
	}
	payload, ok := replayObject(data["payload"])
	if !ok || !hasOnlyKeys(payload, "url") {
		return false
	}
	rawURL, ok := payload["url"].(string)
	if !ok {
		return false
	}
	canonical, valid := canonicalReplayURL(rawURL)
	return valid && canonical == rawURL && rawURL == expectedPageURL
}

func validReplaySnapshotNode(value any, privateAncestor bool) bool {
	return validReplaySnapshotContent(value, privateAncestor, "")
}

func validReplaySnapshotContent(value any, privateAncestor bool, parentTag string) bool {
	node, ok := replayObject(value)
	if !ok {
		return false
	}
	nodeType, ok := replayInteger(node["type"])
	if !ok || nodeType < 0 || nodeType > 5 {
		return false
	}
	if value, exists := node["id"]; exists && !replayID(value, false) {
		return false
	}
	if value, exists := node["rootId"]; exists && !replayID(value, true) {
		return false
	}
	for _, key := range []string{
		"isSVG", "needBlock", "isShadowHost", "isShadow", "isCustom", "isAttachIframe",
	} {
		if value, exists := node[key]; exists && !replayBoolean(value) {
			return false
		}
	}
	switch nodeType {
	case 0:
		if !hasOnlyKeys(
			node, "type", "id", "rootId", "childNodes", "compatMode",
			"isShadowHost", "isShadow", "isAttachIframe",
		) {
			return false
		}
		if mode, exists := node["compatMode"]; exists &&
			mode != "CSS1Compat" && mode != "BackCompat" {
			return false
		}
	case 1:
		if !hasOnlyKeys(node, "type", "id", "rootId", "name", "publicId", "systemId") {
			return false
		}
		name, nameOK := node["name"].(string)
		publicID, publicOK := node["publicId"].(string)
		systemID, systemOK := node["systemId"].(string)
		if !nameOK || !publicOK || !systemOK ||
			strings.ToLower(name) != "html" || publicID != "" || systemID != "" {
			return false
		}
	case 2:
		if !hasOnlyKeys(
			node, "type", "id", "rootId", "tagName", "attributes", "childNodes",
			"isSVG", "needBlock", "isShadowHost", "isShadow", "isCustom",
			"isAttachIframe",
		) {
			return false
		}
		tagName, ok := node["tagName"].(string)
		if !ok || !safeReplayTagName(tagName) {
			return false
		}
	case 3:
		// rrweb marks any node mutation-added directly under a native shadow root with
		// isShadow, regardless of node type — not just elements.
		if !hasOnlyKeys(node, "type", "id", "rootId", "textContent", "isStyle", "isShadow") {
			return false
		}
		if value, exists := node["isStyle"]; exists && !replayBoolean(value) {
			return false
		}
	case 4, 5:
		if !hasOnlyKeys(node, "type", "id", "rootId", "textContent", "isShadow") {
			return false
		}
	}

	privateRegion := privateAncestor
	tag, _ := node["tagName"].(string)
	if tag == "" {
		tag = parentTag
	}
	privateRegion = privateRegion || tag == "input" || tag == "textarea" || tag == "select"
	if attributes, exists := node["attributes"]; exists {
		marker, valid := validReplayAttributes(attributes)
		if !valid {
			return false
		}
		privateRegion = privateRegion || marker
	} else if nodeType == 2 {
		return false
	}
	if content, exists := node["textContent"]; exists {
		text, ok := content.(string)
		if !ok {
			return false
		}
		if nodeType == 3 {
			if tag == "style" || node["isStyle"] == true {
				if !validReplayCSS(text, false) {
					return false
				}
			} else if privateRegion {
				if !maskedReplayText(text) {
					return false
				}
			} else if !validReplayText(text) {
				return false
			}
		}
		if (nodeType == 4 || nodeType == 5) && text != "" {
			return false
		}
	} else if nodeType >= 3 {
		return false
	}
	if children, exists := node["childNodes"]; exists {
		list, ok := children.([]any)
		if !ok {
			return false
		}
		for _, child := range list {
			if !validReplaySnapshotContent(child, privateRegion, tag) {
				return false
			}
		}
	}
	return true
}

func validReplayAttributes(value any) (bool, bool) {
	return validReplayContentAttributes(value)
}

func safeReplayTagName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for index, character := range value {
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		letter := character >= 'a' && character <= 'z'
		digit := index > 0 && character >= '0' && character <= '9'
		hyphen := index > 0 && character == '-'
		if !letter && !digit && !hyphen {
			return false
		}
	}
	return true
}

func maskedReplayText(value string) bool {
	for _, character := range value {
		if character != '*' && !unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

func validReplayIncrementalData(data map[string]any) bool {
	source, ok := replayInteger(data["source"])
	if !ok {
		return false
	}
	switch source {
	case 0:
		return validReplayMutationData(data)
	case 8, 13, 16:
		return validReplayStyleMutation(data, source)
	case 1, 6, 12:
		return validReplayPositionData(data)
	case 2:
		return validReplayMouseInteractionData(data)
	case 3:
		return validReplayNumericData(data, "source", "id", "x", "y")
	case 4:
		return validReplayNumericData(data, "source", "width", "height")
	case 5:
		return validReplayInputData(data)
	case 7:
		return validReplayMediaData(data)
	case 14:
		return validReplaySelectionData(data)
	default:
		return false
	}
}

func validReplayNumericData(data map[string]any, keys ...string) bool {
	if !hasOnlyKeys(data, keys...) {
		return false
	}
	for _, key := range keys {
		value, exists := data[key]
		if !exists {
			continue
		}
		if _, ok := replayNumber(value); !ok {
			return false
		}
	}
	return true
}

func validReplayPositionData(data map[string]any) bool {
	if !hasOnlyKeys(data, "source", "positions") {
		return false
	}
	positions, ok := data["positions"].([]any)
	if !ok {
		return false
	}
	for _, value := range positions {
		position, ok := replayObject(value)
		if !ok || !validReplayNumericData(position, "x", "y", "id", "timeOffset") {
			return false
		}
	}
	return true
}

func validReplayMouseInteractionData(data map[string]any) bool {
	if !hasOnlyKeys(data, "source", "type", "id", "x", "y", "pointerType") {
		return false
	}
	for _, key := range []string{"source", "type", "id", "x", "y"} {
		if value, exists := data[key]; exists {
			if _, ok := replayNumber(value); !ok {
				return false
			}
		}
	}
	if pointer, exists := data["pointerType"]; exists {
		switch pointer {
		case "mouse", "pen", "touch":
		default:
			if _, ok := replayNumber(pointer); !ok {
				return false
			}
		}
	}
	return true
}

func validReplayInputData(data map[string]any) bool {
	if !hasOnlyKeys(data, "source", "id", "text", "isChecked", "userTriggered") {
		return false
	}
	// rrweb stamps id -1 on input events for elements it did not serialize (anything inside
	// a blocked region, e.g. [data-fidibeki-block]). The widget drops these client-side, but
	// they must not poison a batch when an older widget forwards them.
	if value, exists := data["id"]; exists {
		parsed, ok := replayInteger(value)
		if !ok || parsed < -1 {
			return false
		}
	}
	if text, exists := data["text"]; exists {
		value, ok := text.(string)
		if !ok || !maskedReplayText(value) {
			return false
		}
	}
	for _, key := range []string{"isChecked", "userTriggered"} {
		if value, exists := data[key]; exists && !replayBoolean(value) {
			return false
		}
	}
	return true
}

func validReplayMediaData(data map[string]any) bool {
	if !hasOnlyKeys(
		data, "source", "type", "id", "currentTime", "volume", "muted", "playbackRate", "loop",
	) {
		return false
	}
	for _, key := range []string{"source", "type", "id", "currentTime", "volume", "playbackRate"} {
		if value, exists := data[key]; exists {
			if _, ok := replayNumber(value); !ok {
				return false
			}
		}
	}
	for _, key := range []string{"muted", "loop"} {
		if value, exists := data[key]; exists && !replayBoolean(value) {
			return false
		}
	}
	return true
}

func validReplaySelectionData(data map[string]any) bool {
	if !hasOnlyKeys(data, "source", "ranges") {
		return false
	}
	ranges, ok := data["ranges"].([]any)
	if !ok {
		return false
	}
	for _, value := range ranges {
		selected, ok := replayObject(value)
		if !ok || !validReplayNumericData(
			selected, "start", "startOffset", "end", "endOffset",
		) {
			return false
		}
	}
	return true
}

func validReplayMutationData(data map[string]any) bool {
	if !hasOnlyKeys(data, "source", "adds", "removes", "texts", "attributes", "isAttachIframe") {
		return false
	}
	if value, exists := data["isAttachIframe"]; exists && !replayBoolean(value) {
		return false
	}
	if values, exists := data["adds"]; exists {
		adds, ok := values.([]any)
		if !ok {
			return false
		}
		for _, value := range adds {
			added, ok := replayObject(value)
			if !ok || !hasOnlyKeys(added, "parentId", "nextId", "node") ||
				!replayID(added["parentId"], false) ||
				!replayID(added["nextId"], true) ||
				!validReplaySnapshotNode(added["node"], false) {
				return false
			}
		}
	}
	if values, exists := data["removes"]; exists {
		removes, ok := values.([]any)
		if !ok {
			return false
		}
		for _, value := range removes {
			removed, ok := replayObject(value)
			if !ok || !hasOnlyKeys(removed, "parentId", "id", "isShadow") ||
				!replayID(removed["parentId"], false) || !replayID(removed["id"], false) {
				return false
			}
			// rrweb marks removals inside native shadow roots so the player can locate the node.
			if shadow, exists := removed["isShadow"]; exists && !replayBoolean(shadow) {
				return false
			}
		}
	}
	if values, exists := data["texts"]; exists {
		texts, ok := values.([]any)
		if !ok {
			return false
		}
		for _, value := range texts {
			text, ok := replayObject(value)
			if !ok || !hasOnlyKeys(text, "id", "value") || !replayID(text["id"], false) {
				return false
			}
			// rrweb emits null when a text node's content is cleared; null carries no text.
			if text["value"] != nil {
				masked, textOK := text["value"].(string)
				if !textOK || (!validReplayText(masked) && (!strings.Contains(masked, "{") || !validReplayCSS(masked, false))) {
					return false
				}
			}
		}
	}
	if values, exists := data["attributes"]; exists {
		attributes, ok := values.([]any)
		if !ok {
			return false
		}
		for _, value := range attributes {
			change, ok := replayObject(value)
			if !ok || !hasOnlyKeys(change, "id", "attributes") ||
				!replayID(change["id"], false) {
				return false
			}
			if _, valid := validReplayAttributes(change["attributes"]); !valid {
				return false
			}
		}
	}
	return true
}
