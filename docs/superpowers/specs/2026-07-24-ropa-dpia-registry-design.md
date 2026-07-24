# ROPA And DPIA Screening Registry Design

## Status And Scope

A bounded slice of issue #63, implemented locally on 2026-07-24 on top of the already-shipped
provider governance registry (`docs/superpowers/specs/2026-07-24-provider-governance-registry-design.md`)
and the executable retention registry (`docs/superpowers/specs/2026-07-24-executable-retention-registry-design.md`).

This slice does not publish a legal ROPA, does not run or record an actual DPIA decision, does not
assert any lawful basis, and does not deploy, enable, or change any provider/infrastructure
behavior. It builds the machine-readable foundation the founder/legal review needs next: a typed,
code-owned register of processing activities derived from the already-validated retention registry,
plus a per-activity DPIA screening snapshot, with a validator that fails the build the moment the
registry drifts from the retention/provider registries or claims a legal conclusion nothing in the
repository evidences.

## Audit Result

- `backend/internal/privacy/retention_registry.go` already defines 27 personal-data retention
  categories, each with a `Role` (`controller`/`processor`/`controller_and_processor`), a `Purpose`,
  and a validated, non-overlapping set of resources (tables, object prefixes, queues, browser keys,
  and `RetentionProvider` entries).
- `backend/internal/privacy/governance_registry.go` already derives a controller/processor role
  matrix from that same registry and separately tracks the 12 evidenced provider/boundary rows with
  explicit `unknown`/`pending`/`unassigned` legal fields.
- Neither registry groups categories into a business-level "processing activity" (GDPR Article 30's
  actual unit of record), and neither carries a lawful-basis field or a DPIA screening outcome.
  `docs/ai-provider-assessment.md` already contains a prose DPIA-screening discussion for AI
  processing specifically, ending with an explicit unresolved decision ("the privacy owner must
  record one of: 1. a DPIA is required... 2. a DPIA is not required..."). No other retention category
  has an equivalent record, and nothing machine-validates that one exists or stays current.
- There is no HTTP endpoint for the provider governance registry; it is validated purely by
  `make governance-check` (`go test ./internal/privacy/... -run 'Governance' -v`). This slice follows
  the same pattern rather than adding a new API surface, since #63's contract asks only for an
  executable governance foundation, not a new tenant-facing feature.

## Decision

Add `backend/internal/privacy/ropa_registry.go` in the same package, following the existing
registry/validator/summary shape used by `retention_registry.go` and `governance_registry.go`.

### Processing activities group retention categories; they never duplicate them

A `ProcessingActivityRecord` is Article 30's actual unit: a named business activity (for example
"AI-assisted triage" or "Session replay reproduction") that covers one or more of the 27 retention
categories. Every retention category is assigned to exactly one activity — the registry is a
partition, not a re-inventory. `LoadProcessingActivityRegistry` calls `LoadProviderGovernance` first
(which itself validates the retention registry), so a ROPA build can never proceed against an
already-inconsistent retention/provider pair.

Each activity hand-declares `RetentionCategories []string` (validated against
`retentionRegistry()`'s category set: every category must appear, none may repeat or reference an
unknown name) and a `Role` that validation re-derives from those categories' own `Role` fields and
rejects on mismatch — either against the hand-declared value or across categories inside the same
activity. This makes it structurally impossible to declare an activity's role independently of what
the retention registry already says, and it catches the exact failure mode the founder flagged:
someone changing a category's role later without updating the ROPA registry.

`RecipientProviders []string` is never hand-authored. `LoadProcessingActivityRegistry` derives it by
scanning the `RetentionProvider` resources already attached to the activity's member categories, so
recipient/provider evidence can only ever equal what the retention and provider-governance registries
already assert — it cannot invent a boundary or silently drop one when the retention registry
changes.

### New closed classifications, not new legal facts

Two new bounded enums classify *who* and *what*, at the same technical altitude as the existing
`GovernanceBoundaryKind`/`RetentionResourceKind` enums, without asserting any new legal conclusion:

- `ROPADataSubjectClass`: `tenant_members`, `reporters_end_users`, `website_visitors`,
  `platform_admins`, `billing_contacts`.
- `ROPADataCategoryClass`: `identity_contact`, `authentication_security`,
  `content_communications`, `technical_usage`, `media_evidence`, `financial_billing`,
  `consent_evidence`.

Both are restatements of categories already described in `docs/privacy-data-map.md`'s data-category
table, made machine-checkable instead of prose-only.

### Lawful basis stays pending until legal identifies one

`LawfulBasisStatus` is `pending` or `identified`. Every activity in this slice is `pending`, exactly
like every provider in the governance registry started `pending`/`unknown`. `LawfulBasisReason` is
a required, non-placeholder sentence explaining *why* it is still pending (for a `processor`-role
activity, that a customer controller determines the Article 6 basis toward their own data subjects
and Fidibeki's own Article 28 processing basis depends on the not-yet-executed DPA tracked by the
provider governance registry's `ContractStatus`). `LawfulBasisGrounds` (the closed Article 6(1)(a)-(f)
enum) may only be non-empty when the status is `identified`; validation rejects a `pending` entry
that carries grounds, mirroring the existing "pending but carries approval evidence" check for
providers.

### DPIA screening reuses the EDPB WP248 nine criteria as a closed risk-indicator enum

`DPIARiskIndicator` enumerates the nine published EDPB high-risk criteria (evaluation/scoring,
automated decision with legal/similarly-significant effect, systematic monitoring, sensitive/highly
personal data, large-scale processing, matching/combining datasets, vulnerable data subjects,
innovative/new technology, transfer or prevention of a right/service). Tagging which of these apply
to an activity is an engineering/technical observation the codebase already documents in prose (for
example `docs/ai-provider-assessment.md`'s "may process free-form text and rich media at scale...
third-party generative model... incorrect triage could affect how a complaint is handled" maps
directly to `evaluation_or_scoring`, `sensitive_or_highly_personal_data`,
`innovative_or_new_technology`, and `large_scale_processing`). Recording which criteria are
technically present is not the same as deciding whether a DPIA is legally required — exactly the
distinction `docs/ai-provider-assessment.md` already draws: "Engineering cannot determine the legal
threshold alone."

`DPIAOutcome` is `pending`, `required`, or `not_required`. Every activity in this slice is `pending`.
A `required`/`not_required` outcome requires the same non-placeholder owner/reason/review/approval
evidence completeness the provider governance registry already requires for an `approved` provider,
checked with the same staleness window (`governanceReviewStalenessWindow`, reused rather than
redefined) and the same placeholder-detection helper (`governancePlaceholder`, reused rather than
redefined). A `pending` outcome that carries approval evidence is rejected, mirroring the provider
governance registry's identical rule.

### Publication readiness is computed, never declared

`PublicationReady bool` is not a field any activity literal can set; `LoadProcessingActivityRegistry`
computes it after validation from the same completeness rules above (lawful basis identified with
grounds and a real reason; DPIA resolved, not pending, with full approval evidence; owner and review
both current). Because every activity in this slice has a `pending` lawful basis and a `pending` DPIA
outcome, `PublicationReady` computes to `false` for all of them today — the registry cannot be made to
claim publication readiness by editing a boolean; it can only become true once real founder/legal
evidence is recorded in the fields the validator already checks.

## Validator (Release Gate)

`validateProcessingActivityRegistry` fails when:

1. A required field is blank, an enum is out of range, or a bounded list (`DataSubjects`,
   `DataCategories`, `SecurityControls`, `RetentionCategories`) is empty, oversized, or contains a
   duplicate (case-insensitive for free-text lists).
2. `ActivityID` is duplicated.
3. `RetentionCategories` coverage drifts from `retentionRegistry()`: a category is referenced by two
   activities, a category is referenced that does not exist, or a retention category is missing from
   every activity.
4. An activity's declared `Role`, or the `Role` of any of its member retention categories, disagrees
   with the others.
5. `LawfulBasis == pending` carries non-empty `LawfulBasisGrounds`, or `LawfulBasis == identified`
   carries empty grounds or a placeholder `LawfulBasisReason`.
6. `DPIA.Outcome == pending` carries approval evidence (`ApprovedBy`/`ApprovedAt`), or
   `DPIA.Outcome` is `required`/`not_required` without complete, non-placeholder owner, reason,
   review, and approval evidence, or with a stale review, a future timestamp, or an approval dated
   before its review — reusing the same timestamp/placeholder checks already proven for provider
   governance.
7. `DPIARiskIndicator` values are out of the closed nine-criteria enum or duplicated.

This only detects internal/cross-registry drift; like the provider governance registry, it cannot
discover a wholly new processing purpose that nobody records anywhere.

## Extending The Local Gate

`make governance-check` currently runs `go test ./internal/privacy/... -run 'Governance' -v`. The new
tests are named so the existing pattern already matches most of them
(`TestLoadProcessingActivityRegistryGovernance...`), and the Makefile's `-run` pattern is extended to
`'Governance|ROPA|DPIA'` so activity- and DPIA-specific test names do not have to be contorted to
contain the word "Governance." No new command, script, or machine-readable output is added: the
provider governance registry established no such pattern to extend, and inventing one here would be
scope beyond this slice.

## Non-Goals (Unchanged From #63)

No actual provider entity, contract, transfer mechanism, lawful basis, DPIA decision, or legal
approval is recorded. No production/deployment/infrastructure work. No new HTTP endpoint. No change
to existing retention or provider governance behavior — this slice only adds a new file that reads
both.
