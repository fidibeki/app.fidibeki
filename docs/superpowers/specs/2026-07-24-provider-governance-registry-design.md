# Provider Governance Registry Design

## Status And Scope

Implemented locally on 2026-07-24 as the first bounded slice of issue #63, and revised the same day
after review to fix five findings: (1) Cloudflare Turnstile was wrongly excluded as "just CAPTCHA"
despite being a real external personal-data boundary; (2) the registry recorded no per-boundary
service/purpose/personal-data-category evidence; (3) every entry was implicitly labeled a
subprocessor even though several are infrastructure, an internal fallback, or a customer's own
configured destination; (4) approval validation compared placeholder sentinels with exact-string
equality, which whitespace/case variants and unchecked timestamps could bypass; (5) the design
overstated what cross-checking two hand-maintained registries can detect. This revision fixes all
five without deploying, enabling a provider, or publishing any legal/contractual claim.

No deployment, infrastructure change, provider enablement, or credential change is part of this
slice, and no legal, contractual, residency, adequacy, certification, or transfer claim is
published.

This slice does not attempt the full acceptance criteria of #63 (published notices, executed DPA,
ROPA/DPIA workflow, transfer-impact assessments). It builds the machine-readable foundation those
later slices need: a provider/subprocessor registry, a validator that fails the build on incomplete
or overstated entries, and a controller/processor role matrix derived from evidenced code, so that
no later slice can silently drift from what the code actually does.

The code-discovery limitation described below was subsequently narrowed by
`2026-07-24-provider-code-release-gate-design.md`: direct Go/browser dependencies,
standard-library backend HTTP construction, and per-provider source evidence are now checked
against the registry. Arbitrary lower-level transports remain a review boundary.

## Audit Result

`docs/privacy-data-map.md` already lists configurable providers in prose ("Configurable Providers"
section) and explicitly states the list "describes code paths, not the providers enabled in any
particular environment." It is not machine-checked: nothing fails if a new adapter is added to the
code without updating that paragraph, and it carries none of legal entity, processing location,
transfer mechanism, contract/DPA status, retention-review reference, owner, review date, or approval
state.

`backend/internal/privacy/retention_registry.go` (issue #62) already contains a validated,
code-owned list of exactly the external provider boundaries evidenced by the codebase, as
`RetentionProvider` resources inside the `external_communications` and `external_infrastructure`
categories, plus `google_gemini_api` inside `ai_provider_files`:

- `postgresql_runtime`, `redis_asynq`, `s3_compatible_object_storage` — required infrastructure
  (`backend/internal/config/config.go` lists `DATABASE_URL_APP`/`DATABASE_URL_ADMIN`/
  `DATABASE_URL_DIRECT`, `REDIS_URL`, and `S3_ENDPOINT`/`S3_BUCKET`/`S3_ACCESS_KEY`/`S3_SECRET_KEY`
  as unconditionally required in `Config.validate`);
- `resend_transactional_email` and `email_capture_filesystem` — the two `EMAIL_DRIVER` values;
  Resend is only required when `EMAIL_DRIVER=resend`, otherwise mail is written to
  `EMAIL_CAPTURE_DIR` and never leaves the host;
- `google_gemini_api` — required only when `APP_ENV=production` (`config.go` `validate`), and
  additionally gated per widget rule by the AI governance design
  (`docs/superpowers/specs/2026-07-24-ai-privacy-governance-design.md`);
- `stripe_billing_api` — `STRIPE_SECRET_KEY`/`STRIPE_WEBHOOK_SECRET`/`STRIPE_PRICE_*` required only
  in staging/production;
- `sentry_error_telemetry` — `SENTRY_DSN` is read and passed to `sentry.Init` unconditionally
  (`backend/cmd/fidibeki-api/main.go`) but is never a required configuration value; an empty DSN is
  the SDK's documented no-op state;
- `slack_incoming_webhook`, `microsoft_teams_incoming_webhook`, `customer_generic_webhook` — these
  are never global environment configuration. `backend/internal/integration/models.go` defines
  `Kind` (`slack`, `teams`, `webhook`); enablement is a per-tenant `integrations` row created
  through product configuration, not an environment variable.

Cloudflare Turnstile (`TURNSTILE_SECRET_KEY`/`TURNSTILE_SITE_KEY`) was originally excluded here as
"just CAPTCHA." That was wrong: `backend/internal/billing/turnstile.go`
(`TurnstileVerifier.Verify`) posts the visitor's challenge token and `remoteip` (the submitting
request's IP address) to `https://challenges.cloudflare.com/turnstile/v0/siteverify`, and
`web/components/TurnstileWidget.client.vue` loads `challenges.cloudflare.com/turnstile/v0/api.js` in
the browser. An IP address is personal data, and the token/IP leave Fidibeki's infrastructure for
Cloudflare to process — this is an actual external personal-data processing boundary, not merely a
UI widget. `TURNSTILE_SECRET_KEY`/`TURNSTILE_SITE_KEY` are required in staging and production
exactly like Stripe's keys (`backend/internal/config/config.go`, `Config.validate`). It is now a
`RetentionProvider` resource (`cloudflare_turnstile`) under a new `security_verification` retention
category — a semantically correct `controller`/`external_control` category for a security
verification call, distinct from `external_communications` (which is about message delivery) — and
a governance entry classified `service_provider` (see Boundary Kind below).

No other externally reachable adapter exists in the codebase today. This gives exactly the 12
provider adapters #63 asks this slice to cover.

## Decision

Add a second registry, `backend/internal/privacy/governance_registry.go`, in the same package as
the retention registry so both stay compiled and tested together, but scoped to a different
concern: subprocessor/legal governance metadata and the controller/processor role matrix, not
retention duration.

The provider registry is the ground truth for "what the code evidences." It is validated against
the retention registry's existing `RetentionProvider` resource set rather than re-deriving its own
list, so the two registries cannot silently diverge — adding a provider to one without the other
fails a test immediately.

Every entry is code-owned and initialized to the unknown/pending state required by #63's
instructions:

- `LegalEntity`: `"unknown"` unless a specific verified entity string is recorded;
- `ProcessingLocations`: `["unknown"]` unless specific verified locations are recorded;
- `TransferMechanism`: `"unknown"` unless a specific verified Article 46 safeguard is recorded;
- `ContractStatus`: `unknown` (no DPA reference exists in the repository for any provider today);
- `Owner`: `"unassigned"` unless a specific accountable person/role is recorded;
- `ReviewedAt`: `nil` unless a specific review date is recorded;
- `Approval`: `pending` unless every other field above is filled in and current.

No entry in this slice claims `executed` contract status, a specific legal entity, a processing
location, a transfer mechanism, an owner, a review date, or `approved` status, because the
repository contains no such evidence for any of the 12 providers. Populating any of these fields
with real values is future work gated on founder/legal review, exactly as #63's non-goals require.

### Boundary kind: not every entry is a subprocessor

The first revision's doc comment called every entry a "subprocessor," which is false: the registry
also carries Fidibeki's own infrastructure and a purely local fallback. Every entry now carries a
closed `BoundaryKind`:

- `infrastructure_provider` — hosts or runs infrastructure Fidibeki operates directly
  (`postgresql_runtime`, `redis_asynq`, `s3_compatible_object_storage`);
- `service_provider` — an external service used for a specific product function
  (`resend_transactional_email`,
  `google_gemini_api`, `stripe_billing_api`, `sentry_error_telemetry`, `cloudflare_turnstile`);
- `customer_configured_destination` — a recipient the *customer* configures and controls, not
  Fidibeki (`slack_incoming_webhook`, `microsoft_teams_incoming_webhook`,
  `customer_generic_webhook`);
- `internal_runtime_boundary` — never leaves the host Fidibeki operates; not a third party at all
  (`email_capture_filesystem`, the default/local email driver).

`validateProviderGovernanceFields` rejects a blank or out-of-enum `BoundaryKind`, so a new entry
cannot silently default to being mislabeled.

### Service, purpose, and personal-data categories per boundary

Each entry now also carries the per-boundary fields #63 requires beyond code presence:
`DisplayName` (the service name), `Purpose` (one sentence — why the boundary is reached, grounded in
the same code evidence as `CodeEvidence`), and `PersonalDataCategories` (a non-empty, deduplicated
list of at most 32 short controlled phrases (at most 200 bytes each) naming what personal-data
categories cross that specific boundary,
e.g. `cloudflare_turnstile`: `["signup challenge token", "submitting request IP address"]`, drawn
directly from `turnstile.go`). `validateProviderGovernanceFields` rejects a blank `Purpose` and
`validateGovernancePersonalDataCategories` rejects a missing, blank, oversized, or
case/whitespace-normalized duplicate category. `service_provider` deliberately does not itself
assert whether Fidibeki is controller, processor, or subprocessor for that boundary; that legal
classification remains pending review.

### Code presence vs. environment enablement

Each entry separately records `CodeEvidence` (the file/symbol that proves the adapter exists) and
`EnvironmentEnablement`, a closed enum describing how/whether the adapter actually activates in a
running instance:

- `always_required_by_config`: config validation fails at startup without it (Postgres, Redis, S3);
- `required_in_staging_and_production`: config validation fails only outside local/test (Stripe);
- `required_in_production_only`: config validation fails only in production (Gemini), and is
  additionally gated per widget rule at runtime;
- `conditional_on_driver_selection`: activated only when a sibling configuration value selects it
  (Resend requires `EMAIL_DRIVER=resend`; the filesystem capture path is the default/local driver);
- `optional_never_required`: read if present, never required, and a no-op when absent (Sentry);
- `per_tenant_product_configuration`: not a global environment variable at all — a tenant creates
  an `integrations` row to enable it (Slack, Teams, generic webhook).

This directly answers #63's instruction to distinguish code presence (always true for all 12 rows —
the adapter exists and compiles) from environment enablement (which varies per deployment and, for
the three integration kinds, per tenant). Nothing in this registry claims a provider is actually
enabled in any real environment; `EnvironmentEnablement` describes the *mechanism*, never a live
enablement fact, which this repository cannot observe from static code.

## Role Matrix

`GovernanceRoleMatrix()` is derived directly from `retentionRegistry()`'s existing `Category`,
`Label`, `Purpose`, and `Role` fields rather than hand-maintained, so it cannot drift from the
27 already-validated personal-data categories (26 plus the new `security_verification` category —
see Audit Result). Fidibeki is `controller` for workspace/member identity, authentication/security,
access/security audit, billing, and security-verification categories; `processor` for
workflow/feedback, attachments, session replay, AI processing, and integration-delivery categories;
`controller_and_processor` where both roles apply to the same category (for example privacy-rights
case evidence, which covers both a controller's own rights obligations and processor duties toward
a customer's data subjects). This matches the roles #63 asks for explicitly (member/auth/billing/
security/support as controller, customer-controlled widget feedback/evidence as processor) without
introducing a second, independently-maintained source that could disagree with the retention
registry.

## Validator (Release Gate)

`ValidateProviderGovernance` fails when:

1. **Missing required fields** — any of `ProviderID`, `DisplayName`, `AdapterCategory`,
   `BoundaryKind`, `Purpose`, `CodeEvidence`, `EnablementSignal`, `EnvironmentEnablement`,
   `LegalEntity`, `ProcessingLocations`, `TransferMechanism`, `ContractStatus`, `Owner`, or
   `Approval` is blank/zero, or `PersonalDataCategories` is empty, blank, or has a duplicate entry.
   The literal sentinel values `"unknown"`/`"unassigned"`/`pending` satisfy this check; only an
   actually empty value fails it (this check governs *presence*, not whether the value is still a
   placeholder — see point 3).
2. **Provider adapters absent from (or extra in) the registry** — the set of `ProviderID` values
   must equal, exactly, the set of `RetentionProvider` resource names already validated by the
   retention registry. Removing or renaming a covered provider, or adding it to the retention
   registry without adding it here, fails the test. **Limitation:** this check itself only detects
   drift *between the two registries*. The follow-up provider code release gate also rejects
   unclassified direct Go/browser dependencies and new standard-library backend HTTP call sites,
   and verifies current source evidence for every row. It still cannot prove the absence of
   arbitrary lower-level transports; see `2026-07-24-provider-code-release-gate-design.md`.
3. **Falsely approved incomplete entries** — `Approval == approved` requires every governance field
   to be filled with a real (non-unknown/non-placeholder) value: legal entity, processing
   locations, transfer mechanism, executed contract status, a named owner, a review date, an
   approving member, and an approval date. Placeholder detection is normalized — trimmed and
   case-folded — so `" unknown "`, `"UNKNOWN"`, or `"unassigned "` are still rejected as
   placeholders instead of bypassing an exact-string comparison. A `pending` entry may not carry
   an approving member or approval timestamp.
4. **Invalid or stale timestamps** — a non-nil `ReviewedAt` or `ApprovedAt` must be neither the zero
   time nor after the supplied `now`; `ApprovedAt` requires and may not precede `ReviewedAt`; and
   every recorded `ReviewedAt` must be within a 180-day window of `now`. The supplied validation
   time itself must also be non-zero. This applies to every entry with a
   timestamp set, not only approved ones, so a pending entry cannot carry a nonsensical review date
   either. A registry frozen at some past commit does not keep silently reporting an old review as
   current, and a future-dated or zero-valued timestamp cannot be used to dodge staleness.
5. **Unknown boundary kind** — `BoundaryKind` must be one of the four closed values (see Boundary
   kind above); a typo or a new relationship type introduced without updating the enum fails.
6. **Personal-data categories absent from the registry** — every category in `retentionRegistry()`
   must appear exactly once in the role matrix with a valid role. Because the matrix is derived,
   this is structurally guaranteed; the test also asserts the derived count against the retention
   registry's own already-tested category count so a future refactor cannot quietly detach the two.

`LoadProviderGovernance(now time.Time)` runs the validator and returns the assembled registry
(version, providers, role matrix, and approval/contract-status summary counts) or a descriptive
error. It takes `now` as a parameter (never `time.Now()` internally) so tests can exercise staleness
deterministically, matching the existing repository convention of injectable clocks at handler
boundaries.

## Local Release Gate

`make governance-check` runs the governance-registry test package (`go test ./internal/privacy/...
-run Governance`). It is a plain Go test, so it is already part of `make test`/`make test-short`
and CI; the Makefile target exists only so a developer can run this specific gate in isolation
before enabling a new provider or touching `governance_registry.go`, without introducing a new
script, dependency, or CI job. It enables no provider and changes no runtime behavior — it only
fails the local build if the registry becomes incomplete, inconsistent with the retention registry,
falsely approved, or stale.

## Failure And Safety Behavior

- The registry cannot express `approved` for a provider whose legal/contract/location/transfer/
  owner/review fields are still unknown — the validator rejects that state outright.
- The registry cannot omit a provider boundary represented in the retention registry without
  failing a test, because its provider set is checked against that already-validated list rather
  than hand-copied.
- The registry never states an actual processing location, transfer safeguard, legal entity, or
  contract as executed unless a later slice adds real evidence and founder/legal approval; this
  slice adds no such evidence for any of the 12 providers.
- No runtime behavior, credential, or provider enablement changes. This is a static, code-owned
  inventory plus tests; there is no new HTTP endpoint, migration, or UI in this slice.
- Cross-checking against the retention registry catches drift between the two lists. The follow-up
  code gate covers direct dependencies and standard-library HTTP construction, but cannot prove
  the absence of arbitrary lower-level transports (see point 2 under Validator above).

## Verification

Backend unit tests (no database, matching the existing `retention_test.go` style) cover:

- the registry loads and validates for the current 12 providers with the expected pending/unknown
  summary, and every entry carries a non-blank `Purpose` and at least one `PersonalDataCategory`;
- `cloudflare_turnstile` is present, classified `service_provider`, required in staging/production,
  and referenced against the `security_verification` retention category;
- known boundary-kind classifications are correct (infrastructure vs. service provider vs.
  customer-configured destination vs. internal runtime boundary) without treating the boundary
  kind itself as a final controller/processor/subprocessor legal determination;
- the provider set exactly matches the retention registry's `RetentionProvider` resources (added
  and removed cases both fail);
- a missing required field, an unknown boundary kind, and a missing/blank/duplicate/oversized
  personal-data category each fail validation;
- an `approved` entry with any remaining unknown/pending/unassigned field fails as falsely
  approved, including whitespace- or case-varied placeholders (`" unknown "`, `"Unassigned"`) that
  an exact-string comparison would have missed;
- a zero validation time, zero/future timestamps, approval without review, inconsistent pending
  approval evidence, or out-of-order `ReviewedAt`/`ApprovedAt` fails;
- a stale `ReviewedAt` on an otherwise-complete approved entry fails;
- the role matrix covers every retention-registry category exactly once with the matching role,
  including the new `security_verification` category.

The phase ends with `gofmt`, `go vet`, and the full backend test suite (baseline plus the changed
package), a self-review of the diff, and one local commit. No staging validation, deployment, or
new provider enablement is part of this slice.

## Follow-Up Boundary

Later #63 slices, once founder/legal review is available, can:

1. populate real legal entity, processing location, transfer mechanism, and contract/DPA evidence
   for each provider and move entries toward `approved`;
2. add the customer-facing/Fidibeki privacy notices and layered widget contextual notices;
3. build the ROPA/DPIA screening workflow referencing this role matrix and provider registry;
4. surface this registry read-only in the backoffice once it carries real, approved data, mirroring
   the retention registry's backoffice presentation.
