package observability

import "github.com/getsentry/sentry-go"

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
// Kept as-is: EventID, Level, Platform, Timestamp, Release, Environment, ServerName, Sdk, Modules,
// Type, Exception[].Type/Module/ThreadID/Stacktrace/Mechanism (minus Mechanism.Data), and
// Threads[].Stacktrace/Crashed/Current/Name.
//
// Genericized: Message and Exception[].Value (replaced with RedactedTelemetryValue when set).
//
// Always dropped: Request (URL/method/data/query/cookies/headers/env), User (id/email/IP/
// username/name/data), Breadcrumbs (message and data), Contexts, Tags, and Exception[].
// Mechanism.Data — all arbitrary-shaped surfaces nothing in this codebase populates today.
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
		ServerName:  event.ServerName,
		Sdk:         event.Sdk,
		Modules:     event.Modules,
		Type:        event.Type,
		Threads:     scrubThreads(event.Threads),
		Exception:   scrubExceptions(event.Exception),
	}
	if event.Message != "" {
		scrubbed.Message = RedactedTelemetryValue
	}
	return scrubbed
}

func scrubThreads(threads []sentry.Thread) []sentry.Thread {
	if threads == nil {
		return nil
	}
	scrubbed := make([]sentry.Thread, len(threads))
	for i, thread := range threads {
		scrubbed[i] = sentry.Thread{
			ID:         thread.ID,
			Name:       thread.Name,
			Stacktrace: thread.Stacktrace,
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
			Stacktrace: exception.Stacktrace,
			Mechanism:  scrubMechanism(exception.Mechanism),
		}
		if exception.Value != "" {
			next.Value = RedactedTelemetryValue
		}
		scrubbed[i] = next
	}
	return scrubbed
}

func scrubMechanism(mechanism *sentry.Mechanism) *sentry.Mechanism {
	if mechanism == nil {
		return nil
	}
	return &sentry.Mechanism{
		Type:             mechanism.Type,
		Description:      mechanism.Description,
		HelpLink:         mechanism.HelpLink,
		Source:           mechanism.Source,
		Handled:          mechanism.Handled,
		ParentID:         mechanism.ParentID,
		ExceptionID:      mechanism.ExceptionID,
		IsExceptionGroup: mechanism.IsExceptionGroup,
	}
}
