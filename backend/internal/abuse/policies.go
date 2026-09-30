package abuse

import (
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
)

const (
	ReasonSubmissionRateLimited    = "submission_rate_limited"
	ReasonUploadRateLimited        = "upload_rate_limited"
	ReasonReplayRateLimited        = "replay_rate_limited"
	ReasonSpamRejected             = "spam_rejected"
	ReasonPlanQuotaExhausted       = "plan_quota_exhausted"
	ReasonUploadExpired            = "upload_expired"
	ReasonUploadInvalid            = "upload_invalid"
	ReasonUploadUnsafe             = "upload_unsafe"
	ReasonUploadCompletionMismatch = "upload_completion_mismatch"
	ReasonProviderUnavailable      = "provider_temporarily_unavailable"
)

var SubmissionPolicy = Policy{
	Name: "feedback-submission", Version: 1, Reason: ReasonSubmissionRateLimited,
	FailureMode: FailClosed,
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

// Native capture has no browser-enforced Origin or persistent device identity.
// Preserve the existing project/network ceilings and replace claimed
// origin/visitor buckets with the server-minted capture-session boundary.
var MobileSubmissionPolicy = Policy{
	Name: "mobile-feedback-submission", Version: 1, Reason: ReasonSubmissionRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 100, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 10, Period: time.Minute}},
		{Scope: ScopeSession, Window: ratelimit.Window{Limit: 20, Period: time.Hour}},
	},
}

var MobileAttachmentGrantPolicy = Policy{
	Name: "mobile-attachment-grant", Version: 1, Reason: ReasonUploadRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 200, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 20, Period: time.Hour}},
		{Scope: ScopeSession, Window: ratelimit.Window{Limit: 20, Period: time.Hour}},
	},
}

var MobileAttachmentBytePolicy = Policy{
	Name: "mobile-attachment-mebibytes", Version: 1, Reason: ReasonUploadRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 4096, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 256, Period: time.Hour}},
		{Scope: ScopeSession, Window: ratelimit.Window{Limit: 256, Period: time.Hour}},
	},
}

var MobileAttachmentCompletionPolicy = Policy{
	Name: "mobile-attachment-completion", Version: 1, Reason: ReasonUploadRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 300, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 30, Period: time.Minute}},
		{Scope: ScopeSession, Window: ratelimit.Window{Limit: 30, Period: time.Minute}},
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
	Name: "replay-batch", Version: 2, Reason: ReasonReplayRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 2000, Period: time.Hour}},
		// One network must be able to upload a full, bounded 1000-batch session.
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 1000, Period: time.Hour}},
		{Scope: ScopeSession, Window: ratelimit.Window{Limit: 1000, Period: time.Hour}},
	},
}

// Completing an admitted upload must not spend the declaration budget again.
var ReplayBatchCompletionPolicy = Policy{
	Name: "replay-batch-completion", Version: 1, Reason: ReasonReplayRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 2000, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 1000, Period: time.Hour}},
		{Scope: ScopeSession, Window: ratelimit.Window{Limit: 1000, Period: time.Hour}},
	},
}

var ReplayBytePolicy = Policy{
	// Round each batch to KiB so small flushes do not each consume a whole MiB.
	// A separate namespace keeps old MiB counters out of the new unit system.
	Name: "replay-kibibytes", Version: 1, Reason: ReasonReplayRateLimited,
	FailureMode: FailClosed,
	Rules: []Rule{
		{Scope: ScopeProject, Window: ratelimit.Window{Limit: 4096 * 1024, Period: time.Hour}},
		{Scope: ScopeNetwork, Window: ratelimit.Window{Limit: 512 * 1024, Period: time.Hour}},
		{Scope: ScopeSession, Window: ratelimit.Window{Limit: 128 * 1024, Period: time.Hour}},
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
