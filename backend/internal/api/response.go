package api

import (
	"encoding/json"
	"net/http"
)

type ErrorCode string

const (
	CodeBadRequest             ErrorCode = "bad_request"
	CodeUnauthenticated        ErrorCode = "unauthenticated"
	CodeForbidden              ErrorCode = "forbidden"
	CodeTenantSuspended        ErrorCode = "tenant_suspended"
	CodeNotFound               ErrorCode = "not_found"
	CodeUnknownTenant          ErrorCode = "unknown_tenant"
	CodeConflict               ErrorCode = "conflict"
	CodeValidation             ErrorCode = "validation_error"
	CodeRateLimitExceeded      ErrorCode = "rate_limit_exceeded"
	CodePlanLimitExceeded      ErrorCode = "plan_limit_exceeded"
	CodePlanFeatureUnavailable ErrorCode = "plan_feature_unavailable"
	CodeInternal               ErrorCode = "internal_error"
	CodeServiceUnavailable     ErrorCode = "service_unavailable"
)

type Error struct {
	Code          ErrorCode `json:"code"`
	Message       string    `json:"message"`
	Details       any       `json:"details,omitempty"`
	SentryEventID string    `json:"sentry_event_id,omitempty"`
}

type ErrorEnvelope struct {
	Error Error `json:"error"`
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(w http.ResponseWriter, status int, code ErrorCode, message string, details any) {
	WriteErrorWithEventID(w, status, code, message, details, "")
}

func WriteErrorWithEventID(
	w http.ResponseWriter,
	status int,
	code ErrorCode,
	message string,
	details any,
	eventID string,
) {
	WriteJSON(w, status, ErrorEnvelope{Error: Error{
		Code:          code,
		Message:       message,
		Details:       details,
		SentryEventID: eventID,
	}})
}
