package feedback

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/hibiken/asynq"
)

func TestSignatureScanner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		kind        AttachmentKind
		contentType string
		body        []byte
		declared    int64
		wantType    string
		wantReason  string
	}{
		{name: "png", kind: AttachmentScreenshot, contentType: "image/png",
			body: validPNGFixture(), wantType: "image/png"},
		{name: "jpeg", kind: AttachmentScreenshot, contentType: "image/jpeg",
			body: validJPEGFixture(), wantType: "image/jpeg"},
		{name: "webp", kind: AttachmentScreenshot, contentType: "image/webp",
			body: validRIFFFixture("WEBP", "VP8L", []byte{0x2f, 0, 0, 0, 0}), wantType: "image/webp"},
		{name: "webm", kind: AttachmentVideo, contentType: "video/webm",
			body: validWebMFixture(), wantType: "video/webm"},
		{name: "webm block group", kind: AttachmentVideo, contentType: "video/webm",
			body: webMFixtureVariant(2, "A_OPUS", true, true, true, true), wantType: "video/webm"},
		{name: "ogg", kind: AttachmentAudio, contentType: "audio/ogg",
			body: validOggFixture(), wantType: "audio/ogg"},
		{name: "wav", kind: AttachmentAudio, contentType: "audio/wav",
			body: validWAVFixture(), wantType: "audio/wav"},
		{name: "id3 mp3", kind: AttachmentAudio, contentType: "audio/mpeg",
			body: append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), validMP3Fixture()...), wantType: "audio/mpeg"},
		{name: "frame mp3", kind: AttachmentAudio, contentType: "audio/mpeg",
			body: validMP3Fixture(), wantType: "audio/mpeg"},
		{name: "mp3 with tag", kind: AttachmentAudio, contentType: "audio/mpeg",
			body:     append(validMP3Fixture(), append([]byte("TAG"), make([]byte, 125)...)...),
			wantType: "audio/mpeg"},
		{name: "audio mp4", kind: AttachmentAudio, contentType: "audio/mp4",
			body: validMP4Fixture(), wantType: "video/mp4"},
		{name: "replay gzip", kind: AttachmentReplay, contentType: "application/x-gzip",
			body: validGzipFixture(t, []byte(`[{"type":2}]`)), wantType: "application/gzip"},
		{name: "nested replay gzip", kind: AttachmentReplay, contentType: "application/x-gzip",
			body:     validGzipFixture(t, []byte(`{"events":[{"type":2}],"ok":true}`)),
			wantType: "application/gzip"},
		{name: "replay json", kind: AttachmentReplay, contentType: "application/json",
			body: []byte(`[{"type":2},{"type":3}]`), wantType: "application/json"},
		{name: "forged declaration", kind: AttachmentScreenshot, contentType: "image/png",
			body: validJPEGFixture(), wantReason: "signature_mismatch"},
		{name: "wrong kind", kind: AttachmentScreenshot, contentType: "audio/ogg",
			body: validOggFixture(), wantReason: "kind_mismatch"},
		{name: "unknown", kind: AttachmentAudio, contentType: "audio/mpeg",
			body: []byte("not media"), wantReason: "unknown_signature"},
		{name: "malformed replay", kind: AttachmentReplay, contentType: "application/json",
			body: []byte(`{"not":"an array"}`), wantReason: "invalid_json"},
		{name: "signature-only png", kind: AttachmentScreenshot, contentType: "image/png",
			body:       []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
			wantReason: "invalid_structure"},
		{name: "corrupt png checksum", kind: AttachmentScreenshot, contentType: "image/png",
			body: corruptPNGFixture(), wantReason: "invalid_structure"},
		{name: "truncated jpeg", kind: AttachmentScreenshot, contentType: "image/jpeg",
			body: []byte{0xff, 0xd8, 0xff, 0xdb}, wantReason: "invalid_structure"},
		{name: "marker-only jpeg", kind: AttachmentScreenshot, contentType: "image/jpeg",
			body: []byte{
				0xff, 0xd8, 0xff, 0xc0, 0x00, 0x02,
				0xff, 0xda, 0x00, 0x02, 0xff, 0xd9,
			}, wantReason: "invalid_structure"},
		{name: "single mp3 frame", kind: AttachmentAudio, contentType: "audio/mpeg",
			body:       append([]byte{0xff, 0xfb, 0x90, 0x64}, make([]byte, 413)...),
			wantReason: "invalid_structure"},
		{name: "marker-only mp4", kind: AttachmentVideo, contentType: "video/mp4",
			body: []byte{
				0, 0, 0, 16, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0,
				0, 0, 0, 9, 'm', 'o', 'o', 'v', 1,
				0, 0, 0, 9, 'm', 'd', 'a', 't', 1,
			}, wantReason: "invalid_structure"},
		{name: "forged webm", kind: AttachmentVideo, contentType: "video/webm",
			body:       []byte{0x1a, 0x45, 0xdf, 0xa3, 0x80, 0x18, 0x53, 0x80, 0x67, 0x80},
			wantReason: "invalid_structure"},
		{name: "webm without tracks", kind: AttachmentVideo, contentType: "video/webm",
			body:       webMFixtureVariant(2, "A_OPUS", true, true, false, true),
			wantReason: "invalid_structure"},
		{name: "webm without cluster", kind: AttachmentVideo, contentType: "video/webm",
			body:       webMFixtureVariant(2, "A_OPUS", true, true, true, false),
			wantReason: "invalid_structure"},
		{name: "webm invalid track", kind: AttachmentVideo, contentType: "video/webm",
			body:       webMFixtureVariant(3, "A_OPUS", true, true, true, true),
			wantReason: "invalid_structure"},
		{name: "webm missing timecode", kind: AttachmentVideo, contentType: "video/webm",
			body:       webMFixtureVariant(2, "A_OPUS", false, true, true, true),
			wantReason: "invalid_structure"},
		{name: "webm short block", kind: AttachmentVideo, contentType: "video/webm",
			body:       webMFixtureVariant(2, "A_OPUS", true, false, true, true),
			wantReason: "invalid_structure"},
		{name: "primitive gzip replay", kind: AttachmentReplay, contentType: "application/x-gzip",
			body: validGzipFixture(t, []byte(`true`)), wantReason: "invalid_structure"},
		{name: "truncated gzip", kind: AttachmentReplay, contentType: "application/gzip",
			body: []byte{0x1f, 0x8b, 0x08, 0x00}, wantReason: "invalid_structure"},
		{name: "truncated", kind: AttachmentScreenshot, contentType: "image/png",
			body: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, declared: 9,
			wantReason: "size_mismatch"},
		{name: "oversized", kind: AttachmentScreenshot, contentType: "image/png",
			body: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}, declared: 8,
			wantReason: "size_mismatch"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			size := test.declared
			if size == 0 {
				size = int64(len(test.body))
			}
			detected, err := (SignatureScanner{}).Scan(context.Background(), VerificationWork{
				Kind: test.kind, ContentType: test.contentType, SizeBytes: size,
			}, bytes.NewReader(test.body))
			if test.wantReason == "" {
				if err != nil || detected != test.wantType {
					t.Fatalf("Scan() = %q, %v, want %q, nil", detected, err, test.wantType)
				}
				return
			}
			var failure safetyFailure
			if !errors.As(err, &failure) || failure.reason != test.wantReason ||
				err.Error() != test.wantReason {
				t.Fatalf("Scan() error = %v, want safety reason %q", err, test.wantReason)
			}
		})
	}
}

func TestVerificationProcessorMarksOnlyVerifiedObjectsSafe(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	task := VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 2}
	work := VerificationWork{
		VerificationTask: task, FeedbackID: "feedback", ProjectID: "project",
		Kind: AttachmentScreenshot, StorageKey: "object.png", ContentType: "image/png",
		SizeBytes: int64(len(validPNGFixture())), SafetyStatus: AttachmentSafetyQuarantined,
		UploadExpiresAt: now.Add(10 * time.Minute), Attempt: 1,
	}
	store := &verificationStoreStub{work: work}
	storage := &verificationStorageStub{objects: map[string][]byte{
		work.StorageKey: validPNGFixture(),
	}}
	ai := &verifiedAISchedulerStub{}
	current := now
	processor := VerificationProcessor{
		Store: store, Storage: storage, Scanner: SignatureScanner{}, AI: ai,
		Now: func() time.Time { return current },
	}
	payload := []byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":2}`)
	if err := processor.Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if store.safeType != "image/png" || store.safeAt != now || store.rejectedReason != "" {
		t.Fatalf("verification store state = safe %q at %v rejected %q",
			store.safeType, store.safeAt, store.rejectedReason)
	}
	verifiedKey := verifiedAttachmentKey(work.StorageKey, work.Generation, work.Attempt)
	if !storage.removed || store.safeCleanupCompleted ||
		!bytes.Equal(storage.objects[verifiedKey], validPNGFixture()) ||
		ai.attachmentID != task.AttachmentID || ai.feedbackID != work.FeedbackID {
		t.Fatalf("verified side effects = removed %v cleanup %v key %q AI %q/%q",
			storage.removed, store.safeCleanupCompleted, store.safeKey,
			ai.attachmentID, ai.feedbackID)
	}
	storage.objects[work.StorageKey] = []byte("reused staging upload")
	current = work.UploadExpiresAt.Add(uploadCleanupGrace)
	if err := processor.Handler()(
		context.Background(), asynq.NewTask(TaskAttachmentVerify, payload),
	); err != nil {
		t.Fatalf("Handler(final cleanup) error = %v", err)
	}
	if !store.safeCleanupCompleted {
		t.Fatal("verified attachment staging cleanup was not sealed after upload expiry")
	}
}

func TestVerifiedAttachmentStorageKeysRejectMalformedValues(t *testing.T) {
	t.Parallel()
	firstAttempt := verifiedAttachmentKey("attachment.png", 12, 1)
	secondAttempt := verifiedAttachmentKey("attachment.png", 12, 2)
	if firstAttempt == secondAttempt {
		t.Fatalf("verified keys must differ by claim attempt: %q", firstAttempt)
	}
	for _, key := range []string{
		"attachment.png",
		"attachment.png.verified-zero",
		"attachment.png.verified-0",
		"attachment.png.verified-1-zero",
		"attachment.png.verified-1-0",
		"attachment.png.verified-1-2-3",
	} {
		if _, err := stagingAttachmentKey(key); err == nil {
			t.Fatalf("stagingAttachmentKey(%q) error = nil", key)
		}
	}
	if got, err := stagingAttachmentKey("attachment.png.verified-12"); err != nil ||
		got != "attachment.png" {
		t.Fatalf("stagingAttachmentKey(valid) = %q, %v", got, err)
	}
	if got, err := stagingAttachmentKey(secondAttempt); err != nil ||
		got != "attachment.png" {
		t.Fatalf("stagingAttachmentKey(unique attempt) = %q, %v", got, err)
	}
}

func TestVerificationStagingRejectsInvalidSizes(t *testing.T) {
	t.Parallel()
	if _, _, err := verificationReadSeeker(bytes.NewReader([]byte("x")), 0); err == nil {
		t.Fatal("verificationReadSeeker(zero size) error = nil")
	}
	if _, _, err := verificationReadSeeker(bytes.NewReader([]byte("x")), 2); err == nil {
		t.Fatal("verificationReadSeeker(size mismatch) error = nil")
	}
	if _, err := stageVerificationObject(bytes.NewReader(nil), 0); err == nil {
		t.Fatal("stageVerificationObject(zero size) error = nil")
	}
}

func validPNGFixture() []byte {
	var body bytes.Buffer
	_ = png.Encode(&body, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	return body.Bytes()
}

func corruptPNGFixture() []byte {
	body := append([]byte(nil), validPNGFixture()...)
	body[len(body)-1] ^= 0xff
	return body
}

func validJPEGFixture() []byte {
	var body bytes.Buffer
	pixel := image.NewRGBA(image.Rect(0, 0, 1, 1))
	pixel.Set(0, 0, color.White)
	_ = jpeg.Encode(&body, pixel, nil)
	return body.Bytes()
}

func validRIFFFixture(format, chunk string, payload []byte) []byte {
	size := 4 + 8 + len(payload)
	if len(payload)%2 == 1 {
		size++
	}
	body := make([]byte, 8, size+8)
	copy(body[:4], "RIFF")
	binary.LittleEndian.PutUint32(body[4:8], uint32(size))
	body = append(body, format...)
	chunkHeader := make([]byte, 8)
	copy(chunkHeader[:4], chunk)
	binary.LittleEndian.PutUint32(chunkHeader[4:], uint32(len(payload)))
	body = append(body, chunkHeader...)
	body = append(body, payload...)
	if len(payload)%2 == 1 {
		body = append(body, 0)
	}
	return body
}

func validWAVFixture() []byte {
	format := make([]byte, 16)
	binary.LittleEndian.PutUint16(format[0:2], 1)
	binary.LittleEndian.PutUint16(format[2:4], 1)
	binary.LittleEndian.PutUint32(format[4:8], 8000)
	binary.LittleEndian.PutUint32(format[8:12], 8000)
	binary.LittleEndian.PutUint16(format[12:14], 1)
	binary.LittleEndian.PutUint16(format[14:16], 8)
	body := validRIFFFixture("WAVE", "fmt ", format)
	data := []byte{0x80}
	extra := make([]byte, 8)
	copy(extra[:4], "data")
	binary.LittleEndian.PutUint32(extra[4:], uint32(len(data)))
	body = append(body, extra...)
	body = append(body, data...)
	body = append(body, 0)
	binary.LittleEndian.PutUint32(body[4:8], uint32(len(body)-8))
	return body
}

func validWebMFixture() []byte {
	return validWebMFixtureOfSize(0)
}

func validWebMFixtureOfSize(want int) []byte {
	header := ebmlFixture([]byte{0x1a, 0x45, 0xdf, 0xa3},
		ebmlFixture([]byte{0x42, 0x82}, []byte("webm")))
	track := bytes.Join([][]byte{
		ebmlFixture([]byte{0xd7}, []byte{1}),
		ebmlFixture([]byte{0x73, 0xc5}, []byte{1}),
		ebmlFixture([]byte{0x83}, []byte{2}),
		ebmlFixture([]byte{0x86}, []byte("A_OPUS")),
	}, nil)
	tracks := ebmlFixture([]byte{0x16, 0x54, 0xae, 0x6b},
		ebmlFixture([]byte{0xae}, track))
	block := []byte{0x81, 0, 0, 0x80, 1}
	build := func() []byte {
		cluster := ebmlFixture([]byte{0x1f, 0x43, 0xb6, 0x75}, bytes.Join([][]byte{
			ebmlFixture([]byte{0xe7}, []byte{0}),
			ebmlFixture([]byte{0xa3}, block),
		}, nil))
		segment := ebmlFixture([]byte{0x18, 0x53, 0x80, 0x67}, append(tracks, cluster...))
		return append(append([]byte{}, header...), segment...)
	}
	body := build()
	if want > 0 {
		if want < len(body) {
			panic("requested WebM fixture is smaller than the structural fixture")
		}
		baseBlock := append([]byte{}, block...)
		maxPadding := want - len(body) + 16
		for padding := 0; padding <= maxPadding; padding++ {
			block = append(append([]byte{}, baseBlock...), make([]byte, padding)...)
			body = build()
			if len(body) == want {
				return body
			}
		}
		panic("could not construct an exact-size WebM fixture")
	}
	return body
}

func webMFixtureVariant(
	trackType byte,
	codec string,
	timecode bool,
	fullBlock bool,
	includeTracks bool,
	includeCluster bool,
) []byte {
	header := ebmlFixture([]byte{0x1a, 0x45, 0xdf, 0xa3},
		ebmlFixture([]byte{0x42, 0x82}, []byte("webm")))
	track := bytes.Join([][]byte{
		ebmlFixture([]byte{0xd7}, []byte{1}),
		ebmlFixture([]byte{0x83}, []byte{trackType}),
		ebmlFixture([]byte{0x86}, []byte(codec)),
	}, nil)
	segmentBody := make([]byte, 0)
	if includeTracks {
		segmentBody = append(segmentBody, ebmlFixture(
			[]byte{0x16, 0x54, 0xae, 0x6b}, ebmlFixture([]byte{0xae}, track),
		)...)
	}
	if includeCluster {
		cluster := make([]byte, 0)
		if timecode {
			cluster = append(cluster, ebmlFixture([]byte{0xe7}, []byte{0})...)
		}
		block := []byte{0x81, 0, 0, 0x80}
		if fullBlock {
			block = append(block, 1)
		}
		blockGroup := ebmlFixture([]byte{0xa0}, ebmlFixture([]byte{0xa1}, block))
		cluster = append(cluster, blockGroup...)
		segmentBody = append(segmentBody,
			ebmlFixture([]byte{0x1f, 0x43, 0xb6, 0x75}, cluster)...)
	}
	segment := ebmlFixture([]byte{0x18, 0x53, 0x80, 0x67}, segmentBody)
	return append(append([]byte{}, header...), segment...)
}

func ebmlFixture(id, payload []byte) []byte {
	size := len(payload)
	var encodedSize []byte
	switch {
	case size < 127:
		encodedSize = []byte{0x80 | byte(size)}
	case size < 16383:
		encodedSize = []byte{0x40 | byte(size>>8), byte(size)}
	default:
		panic("test EBML payload is too large")
	}
	result := append(append([]byte{}, id...), encodedSize...)
	return append(result, payload...)
}

func validOggFixture() []byte {
	payload := []byte("OpusHead\x01\x01\x00\x00\x80\xbb\x00\x00\x00\x00\x00")
	body := make([]byte, 28, 28+len(payload))
	copy(body[:4], "OggS")
	body[5] = 0x02
	body[26] = 1
	body[27] = byte(len(payload))
	return append(body, payload...)
}

func validMP3Fixture() []byte {
	body := make([]byte, 834)
	copy(body[:4], []byte{0xff, 0xfb, 0x90, 0x64})
	copy(body[417:421], []byte{0xff, 0xfb, 0x90, 0x64})
	return body
}

func validMP4Fixture() []byte {
	init := mp4.CreateEmptyInit()
	track := init.AddEmptyTrack(48000, "audio", "eng")
	if err := track.SetAACDescriptor(2, 48000); err != nil {
		return nil
	}
	segment := mp4.NewMediaSegment()
	fragment, err := mp4.CreateFragment(1, mp4.DefaultTrakID)
	if err != nil {
		return nil
	}
	fragment.AddFullSample(mp4.FullSample{
		Sample: mp4.Sample{Dur: 1024, Size: 4},
		Data:   []byte{0x21, 0x10, 0x04, 0x60},
	})
	segment.AddFragment(fragment)
	var body bytes.Buffer
	if err := init.Encode(&body); err != nil {
		return nil
	}
	if err := segment.Encode(&body); err != nil {
		return nil
	}
	return body.Bytes()
}

func validGzipFixture(t *testing.T, payload []byte) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := gzip.NewWriter(&body)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func TestVerificationProcessorRejectsAndRemovesUnsafeObjects(t *testing.T) {
	t.Parallel()
	task := VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 1}
	work := VerificationWork{
		VerificationTask: task, FeedbackID: "feedback", ProjectID: "project",
		Kind: AttachmentScreenshot, StorageKey: "forged.png", ContentType: "image/png",
		SizeBytes: 4, SafetyStatus: AttachmentSafetyQuarantined,
	}
	storage := &verificationStorageStub{objects: map[string][]byte{
		work.StorageKey: {0xff, 0xd8, 0xff, 0xdb},
	}}
	store := &verificationStoreStub{work: work, removed: func() bool { return storage.removed }}
	processor := VerificationProcessor{Store: store, Storage: storage, Scanner: SignatureScanner{}}
	payload := []byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":1}`)
	if err := processor.Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if !storage.removed || store.rejectedReason != "signature_mismatch" ||
		store.removedBeforeReject || !store.cleanupCompleted {
		t.Fatalf("rejection state = removed %v reason %q removed-before-reject %v cleanup %v",
			storage.removed, store.rejectedReason, store.removedBeforeReject, store.cleanupCompleted)
	}
	if store.safeType != "" {
		t.Fatalf("unsafe attachment marked safe as %q", store.safeType)
	}
}

func TestVerificationProcessorDoesNotSilentlyTrustScannerFailures(t *testing.T) {
	t.Parallel()
	work := VerificationWork{
		VerificationTask: VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 1},
		StorageKey:       "object", SizeBytes: 1, SafetyStatus: AttachmentSafetyQuarantined,
	}
	store := &verificationStoreStub{work: work}
	storage := &verificationStorageStub{objects: map[string][]byte{"object": {1}}}
	processor := VerificationProcessor{
		Store: store, Storage: storage,
		Scanner: scannerFunc(func(context.Context, VerificationWork, io.Reader) (string, error) {
			return "", errors.New("scanner unavailable")
		}),
	}
	payload := []byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":1}`)
	err := processor.Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload))
	if err == nil || store.safeType != "" || store.rejectedReason != "" || storage.removed {
		t.Fatalf("operational failure = %v, safe %q, rejected %q, removed %v",
			err, store.safeType, store.rejectedReason, storage.removed)
	}
}

func TestVerificationProcessorResumesRejectedObjectCleanup(t *testing.T) {
	t.Parallel()
	work := VerificationWork{
		VerificationTask: VerificationTask{
			TenantID: "tenant", AttachmentID: "attachment", Generation: 1,
		},
		StorageKey: "forged", Kind: AttachmentScreenshot, ContentType: "image/png",
		SizeBytes: 4, SafetyStatus: AttachmentSafetyQuarantined,
	}
	store := &verificationStoreStub{work: work}
	storage := &verificationStorageStub{
		objects:   map[string][]byte{"forged": {0xff, 0xd8, 0xff, 0xdb}},
		removeErr: errors.New("storage unavailable"),
	}
	processor := VerificationProcessor{Store: store, Storage: storage, Scanner: SignatureScanner{}}
	task := asynq.NewTask(
		TaskAttachmentVerify,
		[]byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":1}`),
	)
	if err := processor.Handler()(context.Background(), task); err == nil {
		t.Fatal("first cleanup attempt error = nil")
	}
	if store.work.SafetyStatus != AttachmentSafetyRejected || storage.removed {
		t.Fatalf("first cleanup state = %q removed %v", store.work.SafetyStatus, storage.removed)
	}
	storage.removeErr = nil
	if err := processor.Handler()(context.Background(), task); err != nil {
		t.Fatalf("resumed cleanup error = %v", err)
	}
	if !storage.removed || !store.cleanupCompleted {
		t.Fatalf("resumed cleanup = removed %v completed %v", storage.removed, store.cleanupCompleted)
	}
}

func TestVerificationProcessorHandlesWorkerBoundaries(t *testing.T) {
	t.Parallel()

	task := VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 1}
	payload := []byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":1}`)
	work := VerificationWork{
		VerificationTask: task, FeedbackID: "feedback", ProjectID: "project",
		Kind: AttachmentScreenshot, StorageKey: "object.png", ContentType: "image/png",
		SizeBytes: int64(len(validPNGFixture())), SafetyStatus: AttachmentSafetyQuarantined,
	}
	if err := (VerificationProcessor{}).Handler()(
		context.Background(), asynq.NewTask(TaskAttachmentVerify, []byte(`{`)),
	); err == nil || !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("invalid payload error = %v", err)
	}
	if err := (VerificationProcessor{Store: &verificationStoreStub{
		claimErr: ErrNoVerificationWork,
	}}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("no work error = %v", err)
	}
	claimFailure := errors.New("database unavailable")
	if err := (VerificationProcessor{Store: &verificationStoreStub{
		claimErr: claimFailure,
	}}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); !errors.Is(err, claimFailure) {
		t.Fatalf("claim failure = %v", err)
	}
	if err := (VerificationProcessor{
		Store: &verificationStoreStub{work: work},
	}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err == nil {
		t.Fatal("missing scanner error = nil")
	}
	openFailure := errors.New("storage unavailable")
	if err := (VerificationProcessor{
		Store:   &verificationStoreStub{work: work},
		Storage: &verificationStorageStub{openErr: openFailure},
		Scanner: SignatureScanner{},
	}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); !errors.Is(err, openFailure) {
		t.Fatalf("open failure = %v", err)
	}

	aiFailure := errors.New("AI queue unavailable")
	ai := &verifiedAISchedulerStub{err: aiFailure}
	if err := (VerificationProcessor{
		Store: &verificationStoreStub{work: work},
		Storage: &verificationStorageStub{objects: map[string][]byte{
			work.StorageKey: validPNGFixture(),
		}},
		Scanner: SignatureScanner{}, AI: ai,
	}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("optional AI failure = %v", err)
	}
	if ai.attachmentID != task.AttachmentID {
		t.Fatalf("AI scheduler attachment = %q", ai.attachmentID)
	}

	diagnosticFailure := errors.New("diagnostics unavailable")
	rejectedWork := work
	rejectedWork.SizeBytes = int64(len(validJPEGFixture()))
	rejectedStore := &verificationStoreStub{work: rejectedWork}
	if err := (VerificationProcessor{
		Store: rejectedStore,
		Storage: &verificationStorageStub{objects: map[string][]byte{
			work.StorageKey: validJPEGFixture(),
		}},
		Scanner: SignatureScanner{},
		Diagnostics: abuseDiagnosticRecorderFunc(func(
			context.Context,
			abuse.DiagnosticEvent,
		) error {
			return diagnosticFailure
		}),
	}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("optional diagnostic failure = %v", err)
	}
	if rejectedStore.rejectedReason != "signature_mismatch" {
		t.Fatalf("rejected reason = %q", rejectedStore.rejectedReason)
	}
}

type verificationStoreStub struct {
	work                 VerificationWork
	claimErr             error
	safeType             string
	safeKey              string
	safeAt               time.Time
	rejectedReason       string
	removed              func() bool
	removedBeforeReject  bool
	cleanupCompleted     bool
	safeCleanupCompleted bool
}

func (store *verificationStoreStub) Claim(context.Context, VerificationTask) (VerificationWork, error) {
	return store.work, store.claimErr
}

func (store *verificationStoreStub) MarkSafe(
	_ context.Context,
	_ VerificationWork,
	detected string,
	verifiedKey string,
	at time.Time,
) error {
	store.safeType, store.safeKey, store.safeAt = detected, verifiedKey, at
	store.work.SafetyStatus = AttachmentSafetySafe
	store.work.StorageKey = verifiedKey
	return nil
}

func (store *verificationStoreStub) CompleteSafeCleanup(
	context.Context,
	VerificationWork,
	time.Time,
) error {
	store.safeCleanupCompleted = true
	return nil
}

func (store *verificationStoreStub) MarkRejected(
	_ context.Context,
	_ VerificationWork,
	reason string,
	_ *string,
	_ time.Time,
) error {
	store.rejectedReason = reason
	store.work.SafetyStatus = AttachmentSafetyRejected
	if store.removed != nil {
		store.removedBeforeReject = store.removed()
	}
	return nil
}

func (store *verificationStoreStub) CompleteRejectedCleanup(
	context.Context,
	VerificationWork,
	time.Time,
) error {
	store.cleanupCompleted = true
	return nil
}

type verificationStorageStub struct {
	objects   map[string][]byte
	openErr   error
	removeErr error
	removed   bool
}

func (storage *verificationStorageStub) OpenObject(_ context.Context, key string) (io.ReadCloser, error) {
	if storage.openErr != nil {
		return nil, storage.openErr
	}
	return io.NopCloser(bytes.NewReader(storage.objects[key])), nil
}

func (storage *verificationStorageStub) PutObject(
	_ context.Context,
	key string,
	reader io.Reader,
	_ int64,
	_ string,
) error {
	value, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	storage.objects[key] = value
	return nil
}

func (storage *verificationStorageStub) RemoveObject(_ context.Context, key string) error {
	if storage.removeErr != nil {
		return storage.removeErr
	}
	delete(storage.objects, key)
	storage.removed = true
	return nil
}

type verifiedAISchedulerStub struct {
	attachmentID string
	feedbackID   string
	err          error
}

func (scheduler *verifiedAISchedulerStub) ScheduleAttachment(
	_ context.Context,
	attachmentID string,
	feedbackID string,
) error {
	scheduler.attachmentID, scheduler.feedbackID = attachmentID, feedbackID
	return scheduler.err
}

type scannerFunc func(context.Context, VerificationWork, io.Reader) (string, error)

func (scanner scannerFunc) Scan(
	ctx context.Context,
	work VerificationWork,
	reader io.Reader,
) (string, error) {
	return scanner(ctx, work, reader)
}

type abuseDiagnosticRecorderFunc func(context.Context, abuse.DiagnosticEvent) error

func (record abuseDiagnosticRecorderFunc) Record(
	ctx context.Context,
	event abuse.DiagnosticEvent,
) error {
	return record(ctx, event)
}
