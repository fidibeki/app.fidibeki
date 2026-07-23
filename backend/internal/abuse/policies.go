package abuse

import (
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
)

const (
	ReasonSubmissionRateLimited = "submission_rate_limited"
	ReasonUploadRateLimited     = "upload_rate_limited"
	ReasonReplayRateLimited     = "replay_rate_limited"
	ReasonSpamRejected          = "spam_rejected"
	ReasonPlanQuotaExhausted    = "plan_quota_exhausted"
	ReasonUploadExpired         = "upload_expired"
	ReasonUploadInvalid         = "upload_invalid"
	ReasonUploadUnsafe          = "upload_unsafe"
	ReasonProviderUnavailable   = "provider_temporarily_unavailable"
)

var SubmissionPolicy = Policy{
	Name: "feedback-submission", Version: 1, Reason: ReasonSubmissionRateLimited,
	FailureMode: FailOpen,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 100, Period: time.Hour}},
		{Scope: ScopeOrigin, Window: ratelimit.Window{Limit: 60, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 10, Period: time.Minute}},
		{Scope: ScopeVisitor, Window: ratelimit.Window{Limit: 20, Period: time.Hour}, Optional: true},
	},
}

var AttachmentGrantPolicy = Policy{
	Name: "attachment-grant", Version: 1, Reason: ReasonUploadRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 200, Period: time.Hour}},
		{Scope: ScopeOrigin, Window: ratelimit.Window{Limit: 120, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 20, Period: time.Hour}},
		{Scope: ScopeVisitor, Window: ratelimit.Window{Limit: 20, Period: time.Hour}, Optional: true},
	},
}

var AttachmentBytePolicy = Policy{
	Name: "attachment-mebibytes", Version: 1, Reason: ReasonUploadRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 4096, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 256, Period: time.Hour}},
		{Scope: ScopeVisitor, Window: ratelimit.Window{Limit: 256, Period: time.Hour}, Optional: true},
	},
}

var AttachmentCompletionPolicy = Policy{
	Name: "attachment-completion", Version: 1, Reason: ReasonUploadRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 300, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 30, Period: time.Minute}},
	},
}

var ReplayStartPolicy = Policy{
	Name: "replay-start", Version: 1, Reason: ReasonReplayRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 120, Period: time.Hour}},
		{Scope: ScopeOrigin, Window: ratelimit.Window{Limit: 60, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 20, Period: time.Hour}},
		{Scope: ScopeVisitor, Window: ratelimit.Window{Limit: 20, Period: time.Hour}, Optional: true},
	},
}

var ReplayBatchPolicy = Policy{
	Name: "replay-batch", Version: 1, Reason: ReasonReplayRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 2000, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 200, Period: time.Hour}},
		{Scope: ScopeSession, Window: ratelimit.Window{Limit: 1000, Period: time.Hour}},
	},
}

var ReplayBytePolicy = Policy{
	Name: "replay-mebibytes", Version: 1, Reason: ReasonReplayRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 4096, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 512, Period: time.Hour}},
		{Scope: ScopeSession, Window: ratelimit.Window{Limit: 128, Period: time.Hour}},
	},
}

var AIBudgetPolicy = Policy{
	Name: "ai-daily-work", Version: 1, Reason: ReasonProviderUnavailable,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeTenant, Window: ratelimit.Window{Limit: 5000, Period: 24 * time.Hour}},
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 2000, Period: 24 * time.Hour}},
	},
}
