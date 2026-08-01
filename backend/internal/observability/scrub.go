package observability

import (
	"regexp"
	"strconv"

	"github.com/getsentry/sentry-go"
)

var replaySemanticVersion = regexp.MustCompile(`^(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})(?:-[0-9A-Za-z.-]{1,32})?$`)

// RedactedTelemetryValue replaces every free-text Sentry value ScrubEvent cannot prove is free of
// customer data: a recovered panic's message/error text. Go error strings are unstructured and may
// interpolate request-scoped data, so the type and stack trace — not the literal text — carry the
// evidence that survives.
const RedactedTelemetryValue = "[fidibeki] value removed before Sentry send; see exception type and stacktrace"

// ScrubEvent is the Sentry BeforeSend hook for every hub InitSentry configures, including the
// per-panic hub SentryPanicReporter clones. It reconstructs the outgoing event from an explicit
// allowlist of known-safe fields instead of deleting known-bad ones from the original, so a field
// this function does not name — whether a future call site sets it or a future sentry-go upgrade
// adds it to the protocol — is absent from the scrubbed event by construction.
//
// Kept as-is: EventID, Level, Platform, Timestamp, Release, Environment, Type,
// Exception[].Type/Module/ThreadID, structural stack-frame fields, mechanism type/handled state,
// and Threads[].ID/Stacktrace/Crashed/Current.
//
// Genericized: Message and Exception[].Value (replaced with RedactedTelemetryValue when set).
//
// Always dropped: Request (URL/method/data/query/cookies/headers/env), User (id/email/IP/
// username/name/data), Breadcrumbs (message and data), Contexts, non-allowlisted tags, ServerName,
// Sdk, Modules, thread names, frame absolute paths/source context/locals, and mechanism free
// text/data. Only bounded replay diagnostic tags survive.
func ScrubEvent(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil {
		return nil
	}

	scrubbed := &sentry.Event{
		EventID:     event.EventID,
		Level:       event.Level,
		Platform:    event.Platform,
		Timestamp:   event.Timestamp,
		Release:     event.Release,
		Environment: event.Environment,
		Type:        event.Type,
		Threads:     scrubThreads(event.Threads),
		Exception:   scrubExceptions(event.Exception),
		Tags:        scrubReplayTags(event.Tags),
	}
	if scrubbed.Tags["operation"] != "" && scrubbed.Tags["failure_code"] != "" {
		scrubbed.Fingerprint = replayFailureFingerprint(scrubbed.Tags)
	}
	if event.Message != "" {
		scrubbed.Message = RedactedTelemetryValue
	}
	return scrubbed
}

func scrubReplayTags(tags map[string]string) map[string]string {
	if tags["subsystem"] != "replay" {
		return nil
	}
	kept := map[string]string{"subsystem": "replay"}
	keepReplayTag(kept, tags, "operation", func(value string) bool {
		return value == "verify_batch" || value == "merge"
	})
	keepReplayTag(kept, tags, "failure_code", func(value string) bool {
		switch value {
		case "contract_violation", "unsafe_payload", "upload_expired", "processing_failed", "unavailable":
			return true
		default:
			return false
		}
	})
	keepReplayTag(kept, tags, "violation_code", func(value string) bool {
		return value == "media_fields_not_supported"
	})
	keepReplayTag(kept, tags, "rrweb_version", func(value string) bool {
		return len(value) <= 64 && replaySemanticVersion.MatchString(value)
	})
	keepReplayTag(kept, tags, "event_type", func(value string) bool {
		parsed, err := strconv.Atoi(value)
		return err == nil && parsed >= 0 && parsed <= 5
	})
	keepReplayTag(kept, tags, "incremental_source", func(value string) bool {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return false
		}
		switch parsed {
		case 0, 1, 2, 3, 4, 5, 6, 7, 12, 14:
			return true
		default:
			return false
		}
	})
	keepReplayTag(kept, tags, "retry_exhausted", func(value string) bool {
		return value == "true" || value == "false"
	})
	return kept
}

func keepReplayTag(
	destination map[string]string,
	source map[string]string,
	key string,
	valid func(string) bool,
) {
	if value, exists := source[key]; exists && valid(value) {
		destination[key] = value
	}
}

func scrubThreads(threads []sentry.Thread) []sentry.Thread {
	if threads == nil {
		return nil
	}
	scrubbed := make([]sentry.Thread, len(threads))
	for i, thread := range threads {
		scrubbed[i] = sentry.Thread{
			ID:         thread.ID,
			Stacktrace: scrubStacktrace(thread.Stacktrace),
			Crashed:    thread.Crashed,
			Current:    thread.Current,
		}
	}
	return scrubbed
}

func scrubExceptions(exceptions []sentry.Exception) []sentry.Exception {
	if exceptions == nil {
		return nil
	}
	scrubbed := make([]sentry.Exception, len(exceptions))
	for i, exception := range exceptions {
		next := sentry.Exception{
			Type:       exception.Type,
			Module:     exception.Module,
			ThreadID:   exception.ThreadID,
			Stacktrace: scrubStacktrace(exception.Stacktrace),
			Mechanism:  scrubMechanism(exception.Mechanism),
		}
		if exception.Value != "" {
			next.Value = RedactedTelemetryValue
		}
		scrubbed[i] = next
	}
	return scrubbed
}

func scrubStacktrace(stacktrace *sentry.Stacktrace) *sentry.Stacktrace {
	if stacktrace == nil {
		return nil
	}
	frames := make([]sentry.Frame, len(stacktrace.Frames))
	for i, frame := range stacktrace.Frames {
		frames[i] = sentry.Frame{
			Function:   frame.Function,
			Symbol:     frame.Symbol,
			Module:     frame.Module,
			Filename:   frame.Filename,
			Lineno:     frame.Lineno,
			Colno:      frame.Colno,
			InApp:      frame.InApp,
			Package:    frame.Package,
			Platform:   frame.Platform,
			StackStart: frame.StackStart,
		}
	}
	return &sentry.Stacktrace{
		Frames:        frames,
		FramesOmitted: append([]uint(nil), stacktrace.FramesOmitted...),
	}
}

func scrubMechanism(mechanism *sentry.Mechanism) *sentry.Mechanism {
	if mechanism == nil {
		return nil
	}
	return &sentry.Mechanism{
		Type:             mechanism.Type,
		Handled:          copyPointer(mechanism.Handled),
		ParentID:         copyPointer(mechanism.ParentID),
		ExceptionID:      mechanism.ExceptionID,
		IsExceptionGroup: mechanism.IsExceptionGroup,
	}
}

func copyPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
