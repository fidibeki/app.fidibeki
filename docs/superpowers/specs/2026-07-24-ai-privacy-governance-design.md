# AI Privacy Governance Design

**Date:** 2026-07-24  
**Issue:** [#61](https://github.com/acidiney/app.fidibeki/issues/61)  
**Environment:** staging only

## Decision

AI capability in a commercial plan is not authorization to process reporter content. Each widget
rule has an explicit AI processing configuration. Existing and new rules default to disabled.
Enabling requires an authorized operator to acknowledge the provider, purpose, input categories,
customer responsibilities, and policy version.

The selected approach is a rule-owned configuration plus a server-owned processing ledger:

- the rule is the collection-time policy boundary and is already referenced by every Feedback;
- the plan remains a prerequisite, never an implicit opt-in;
- the backend rechecks the current rule policy before scheduling, claiming, and persisting work;
- every provider result is normalized and redacted by the backend before persistence;
- a non-identifying processing ledger records governance evidence without duplicating raw content.

## Alternatives Considered

### Project-only switch

A project switch is simpler but cannot express different collection policies across rules. It also
makes disclosure inaccurate when only some feedback experiences send media or text to Gemini.

### Snapshot authorization on Feedback

A snapshot is useful for evidence but unsafe as the enforcement source: disabling AI would not stop
queued or in-flight work. The current rule remains authoritative. The processing ledger records the
policy version used for completed or discarded work.

### Model-only redaction

Prompting Gemini to redact is useful defense in depth but cannot be trusted. Malformed JSON, prompt
injection, or a model regression could persist identifiers. The backend therefore applies its own
deterministic redaction and rejects output that cannot satisfy the policy.

## Rule Configuration

`widget_rules.ai_processing` is a constrained JSON object:

- `enabled`: explicit opt-in, default `false`;
- `policy_version`: the Fidibeki policy version acknowledged by the operator;
- `provider`: fixed to `google_gemini`;
- `purposes`: fixed to advisory transcription, cleanup, moderation, sentiment, and triage;
- `input_categories`: a subset of `feedback_text`, `screenshot`, `audio`, and `video`;
- `acknowledged_at` and `acknowledged_by_member_id`;
- `privacy_policy_url`: the customer's HTTPS privacy information;
- `sensitive_data_mode`: fixed initially to `prohibit_and_quarantine`.

The API ignores caller-supplied actor and timestamp values. Enabling stamps both on the server.
Changing the enabled state or acknowledged contract appends a tenant access event containing IDs,
versions, categories, and before/after policy state, never reporter content.

Existing rules migrate to disabled. This intentionally stops implicit processing until an operator
reviews and enables the policy.

## Runtime Enforcement

Feedback text and safe non-replay attachments begin as `skipped` unless both the tenant plan and
the current rule policy allow their input category. Enabling AI does not retroactively process
historical skipped data; a future explicit reprocess action may do that.

The scheduler joins Feedback to its current widget rule. Claim operations repeat that check to
invalidate stale queue entries. Completion checks it once more in the same transaction:

- if still enabled, the safe result and ledger entry are committed;
- if disabled while the provider call was in progress, the result is discarded and work becomes
  `skipped`;
- replay, identity fields, reporter email, analytics IDs, automatic metadata, and unsupported
  attachments never enter a provider request.

Disabling a rule atomically marks its pending/processing text and attachments as skipped and clears
their queue markers. Already running calls may finish at the provider, but their output cannot be
persisted. Provider file cleanup remains mandatory.

## Redaction And Sensitive Data

Gemini receives an instruction to replace sensitive values with the exact literal `[REDACTED]`.
Before a provider request, the backend minimizes written text using the same deterministic policy.
Media cannot be transformed safely without another processing service, so it is allowed only under
the acknowledged media category and the provider instruction.

After the provider response, one server-side redactor processes transcription, cleaned text, title,
category, and every string recommendation. It detects at least:

- email and network addresses;
- phone numbers and postal-address patterns;
- credentials, tokens, secrets, and authentication material;
- payment, bank, tax, account, and government identifiers;
- explicitly stated names and sensitive-category statements covering health, politics, religion,
  sexuality, biometrics, employment, and children.

The persisted manifest contains only category counts and the redaction policy version. It never
contains removed values, reversible mappings, hashes, or raw model output. Invalid model JSON,
unsupported output, or a result that cannot be validated is quarantined and not exposed.

## Processing Ledger

`ai_processing_runs` records one row per claimed generation/attempt:

- tenant, project, Feedback, optional attachment, rule, and policy version;
- provider `google_gemini`, configured model, processing purpose, and input category;
- status (`processing`, `completed`, `failed`, `discarded`, `quarantined`);
- redaction manifest, start/completion timestamps, and a bounded non-content reason.

Gemini upload deletion evidence remains in `ai_gemini_files`; the run links to its resource where
applicable. Source and derived data use the existing privacy export, restriction, retention, and
deletion paths. The ledger is evidence, not another source of reporter content.

## Operator And Reporter Experience

The rule editor exposes a quiet AI analysis section. Turning it on opens a confirmation dialog that
states the provider, advisory purposes, selectable supported inputs, sensitive-data prohibition,
customer responsibility, retention/deletion behavior, and privacy URL. Save remains blocked until
the complete current acknowledgment is accepted.

The public widget contract exposes only safe disclosure data. When enabled, the form shows localized
English or Portuguese layered copy explaining that the selected content types may be processed by
Google Gemini for advisory analysis, with the customer's privacy link. Reporter-update consent is
separate and is never reused as AI authorization.

AI recommendations remain advisory and cannot create Issues, change status, assign members, or send
external messages without a human action.

## Documentation Boundary

`docs/privacy-data-map.md` will identify Google Gemini as a processor/subprocessor boundary and
record the technical facts the application can prove. A separate provider assessment records
location, subprocessors, transfer mechanism, retention, training restrictions, incident terms,
change notification, and DPIA screening. Legal conclusions must be verified from the executed
customer/provider terms and are not inferred from code.

## Test Strategy

- migration replay and RLS/isolation tests;
- validation and authorization tests for enablement acknowledgment;
- scheduler, stale queue, disablement race, and in-flight completion tests;
- request-capture sentinels proving identity, metadata, replay IDs, and tokens are absent;
- adversarial provider fixtures covering prompt injection and malformed JSON;
- redaction fixtures for identifiers, secrets, finance, names, and sensitive categories;
- API/export/integration assertions that raw sentinels never appear;
- widget unit and browser tests for localized disclosure;
- backoffice desktop/mobile E2E for enable, confirm, disable, and advisory-only behavior;
- final validation on staging only.

## Implementation Slices

1. Add the safe rule policy, server-owned acknowledgment audit, and three enforcement checks.
2. Add deterministic minimization/redaction, processing evidence, quarantine, and race coverage.
3. Add the operator confirmation UI and public localized widget disclosure.
4. Extend privacy/export/retention paths, provider/DPIA documentation, full local gates, and staging
   E2E validation.

