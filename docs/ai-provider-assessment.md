# Google Gemini AI Provider Assessment

**Technical review:** 2026-07-24
**Scope:** Fidibeki advisory feedback analysis, staging only
**Decision:** disabled by default; broad enablement blocked pending the approvals below

This is an engineering and DPIA-screening record, not legal advice or proof of a provider account's
configuration. Contract owners must verify the executed terms and the actual Google project before
changing the decision.

## Processing Boundary

Fidibeki uses the Google Gemini Developer API through the official Go SDK with
`BackendGeminiAPI`. The only operations in scope are:

- `generateContent` for advisory transcription, cleanup, moderation, sentiment, title, category,
  actionability, and priority suggestions;
- the Files API for an explicitly allowed screenshot, audio recording, or video recording;
- immediate Files API deletion after analysis, with durable retries and deletion evidence.

The implementation does not use Google Search or Maps grounding, tuning, datasets, explicit context
caching, the stateful Interactions API, or the Live API. AI output cannot create an Issue or change a
workflow without a later human action.

## Data Sent

| Input | Sent only when | Minimization |
|---|---|---|
| Feedback text | Matching rule explicitly allows `feedback_text` | Deterministic server redaction before request; wrapped as untrusted feedback |
| Screenshot | Matching rule explicitly allows `screenshot` and the attachment passed safety checks | Constant provider display name; no Fidibeki resource ID |
| Audio | Matching rule explicitly allows `audio` and the attachment passed safety checks | Constant provider display name; no Fidibeki resource ID |
| Video | Matching rule explicitly allows `video` and the attachment passed safety checks | Constant provider display name; no Fidibeki resource ID |

Reporter email/contact consent, page URL, referrer, user agent, device/viewport, replay, DOM
snapshot, analytics identifiers, tenant/project/member IDs, and queue/task IDs are not provider
inputs. Media itself can still contain personal data that deterministic pre-redaction cannot remove;
this is why media categories require separate explicit selection and why sensitive data is
prohibited.

## Technical Controls

- Existing and new rules start with AI disabled; plan capability is only a prerequisite.
- An authorized operator accepts policy version `2026-07-24`, provider, fixed advisory purposes,
  selected inputs, customer responsibility, sensitive-data prohibition, and an HTTPS privacy URL.
- The backend rechecks the current rule at scheduling, claim, and persistence. Disabling AI marks
  queued work skipped and rejects the result of an already running call.
- Gemini is instructed to use the exact `[REDACTED]` marker. A separate server redactor protects
  every derived text field and accepts only bounded enums/flags. Unsafe or malformed output is
  quarantined.
- The processing ledger contains resource IDs, provider/model/purpose/category, policy version,
  timestamps, status, category counts, and bounded non-content failures. It contains no prompts,
  raw results, removed values, email, page URL, analytics identity, or provider file name.
- Uploaded provider files are deleted immediately after the call. Failure is retried and visible in
  the deletion ledger. Google's Files API documents automatic deletion after 48 hours, which is
  treated only as a provider fallback:
  <https://ai.google.dev/gemini-api/docs/files>.

## Current Provider Statements To Verify

Google's published Gemini API terms distinguish Paid and Unpaid Services. For Paid Services, Google
states that prompts and responses are not used to improve its products and are processed under its
data-processing addendum. It also states that prompts/responses may be logged for a limited period
for abuse monitoring and required disclosures:
<https://ai.google.dev/gemini-api/terms>.

Google's logging documentation says `generateContent` request logging is off by default unless
enabled at request/project level, while optional logs or datasets can create additional retention
and sharing:
<https://ai.google.dev/gemini-api/docs/logs-policy>.

Google's ZDR documentation says paid usage still has limited retention scenarios, Files API storage
is independent, and project approval/configuration is required for the documented ZDR treatment:
<https://ai.google.dev/gemini-api/docs/zdr>.

These are public statements reviewed on the date above. They do not prove Fidibeki's billing tier,
project settings, DPA acceptance, ZDR approval, region, subprocessors, or transfer mechanism.

## Blocking Approval Checklist

- [ ] Prove that the exact Gemini API project used by staging has active billing and is treated as a
  Paid Service. Never send customer feedback through unpaid quota outside the EEA-specific terms
  without a separate legal decision.
- [ ] Verify `generateContent` logging is off, no logs are saved to datasets, and no provider data
  sharing/feedback contribution is enabled.
- [ ] Record the executed Google data-processing addendum, controller/processor roles,
  subprocessors, hosting/processing locations, cross-border transfer mechanism, security schedule,
  breach terms, deletion assistance, audit rights, and change-notification process.
- [ ] Decide whether to request/require provider ZDR and retain evidence of the result.
- [ ] Confirm the configured model and API are generally available and approved for customer data;
  preview/experimental models require a separate review.
- [ ] Approve customer-facing disclosure, lawful basis/consent position, sensitive-data
  prohibition, and the customer DPA/subprocessor notice.
- [ ] Approve retention for the category-only manifest and processing/deletion evidence.
- [ ] Run the staging sentinel test and confirm no provider request contains identity, resource IDs,
  page metadata, replay data, secrets, or the original detected sensitive values.

Broad staging enablement remains blocked until every item above has an owner, evidence, and recorded
approval. Production remains out of scope until the product owner explicitly authorizes it.

## DPIA Screening

The feature may process free-form text and rich media at scale, reporters may include special
category or third-party data despite instructions, a third-party generative model is involved, and
incorrect triage could affect how a complaint is handled. Those factors require a documented DPIA
screening before broad enablement. Engineering cannot determine the legal threshold alone.

The current controls reduce exposure but do not remove the need for that decision: opt-in per rule,
category selection, layered notice, pre/post redaction, sensitive-data quarantine, no automatic
action, no identity metadata, immediate file deletion, policy race protection, audit evidence,
export, and erasure. The privacy owner must record one of:

1. a DPIA is required, with scope, owner, mitigations, consultation, and approval;
2. a DPIA is not required, with jurisdiction-specific reasoning and residual-risk acceptance.
