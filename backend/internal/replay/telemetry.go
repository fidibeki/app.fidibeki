package replay

import (
	"context"
	"strconv"
)

type FailureOperation string

const (
	FailureOperationVerifyBatch FailureOperation = "verify_batch"
	FailureOperationMerge       FailureOperation = "merge"
)

// FailureReport is deliberately limited to server-defined enums and bounded numeric metadata.
// It must never grow identifiers, URLs, storage keys, event bodies, or arbitrary error text.
type FailureReport struct {
	Operation         FailureOperation
	Code              FailureCode
	Violation         ViolationCode
	RRWebVersion      string
	EventType         int
	IncrementalSource int
	RetryExhausted    bool
}

type FailureReporter interface {
	ReportReplayFailure(context.Context, FailureReport)
}

func (report FailureReport) TelemetryTags() map[string]string {
	if !validFailureOperation(report.Operation) || !validFailureCode(report.Code) ||
		(report.Violation != "" && !validViolationCode(report.Violation)) {
		return nil
	}
	tags := map[string]string{
		"subsystem":       "replay",
		"operation":       string(report.Operation),
		"failure_code":    string(report.Code),
		"retry_exhausted": strconv.FormatBool(report.RetryExhausted),
	}
	if report.Violation != "" {
		tags["violation_code"] = string(report.Violation)
	}
	if report.RRWebVersion != "" {
		tags["rrweb_version"] = report.RRWebVersion
	}
	if report.Operation == FailureOperationVerifyBatch && report.Violation != "" {
		tags["event_type"] = strconv.Itoa(report.EventType)
		tags["incremental_source"] = strconv.Itoa(report.IncrementalSource)
	}
	return tags
}

func validFailureOperation(operation FailureOperation) bool {
	return operation == FailureOperationVerifyBatch || operation == FailureOperationMerge
}

func validFailureCode(code FailureCode) bool {
	switch code {
	case FailureContractViolation, FailureUnsafePayload, FailureUploadExpired,
		FailureProcessingFailed, FailureUnavailable:
		return true
	default:
		return false
	}
}

func validViolationCode(code ViolationCode) bool {
	return code == ViolationMediaFieldsNotSupported
}
