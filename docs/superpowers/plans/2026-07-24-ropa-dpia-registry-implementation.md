# ROPA And DPIA Screening Registry Implementation

> Source design:
> `docs/superpowers/specs/2026-07-24-ropa-dpia-registry-design.md`

## Durable Decisions

- Processing activities partition the 27 existing retention categories; no category may be
  uninventoried, duplicated, or re-described outside the retention registry it already lives in.
- `Role` and `RecipientProviders` are derived from/validated against the retention and provider
  governance registries, never independently hand-maintained.
- Lawful basis and DPIA outcome default to `pending`/`pending` for every activity; nothing in this
  slice asserts a real lawful basis, DPIA decision, or publication-ready status.
- `PublicationReady` is computed from completeness rules, never a literal field.
- No new HTTP endpoint; validated the same way the provider governance registry is, through
  `make governance-check`.
- No production/deployment/infrastructure change.

## Tracer Bullet

1. Add closed enums: `ROPADataSubjectClass`, `ROPADataCategoryClass`, `LawfulBasisStatus`,
   `LawfulBasisGround`, `DPIAOutcome`, `DPIARiskIndicator`.
2. Add `DPIAScreening` and `ProcessingActivityRecord` types plus `ProcessingActivityRegistry` and
   `ROPASummary`.
3. Hand-author 19 processing activities covering all 27 retention categories exactly once, each with
   purpose, data-subject/category classes, security-control references, `pending` lawful basis with
   an explanatory reason, and a `pending` DPIA screening carrying only technically-evidenced risk
   indicators (drawn from existing docs, not invented).
4. Implement `LoadProcessingActivityRegistry(now)`: call `LoadProviderGovernance(now)` first, derive
   `RecipientProviders` per activity from retention resources, validate, compute
   `PublicationReady`/`Summary`.
5. Implement `validateProcessingActivityRegistry` covering field completeness, duplicate/unknown
   references, retention-category coverage (exactly-once), role-consistency-with-retention-registry,
   lawful-basis consistency, and DPIA evidence completeness/staleness/consistency, reusing
   `governancePlaceholder` and `governanceReviewStalenessWindow`.
6. Add focused tests in `ropa_registry_test.go` named so `make governance-check`'s pattern covers
   them; extend the Makefile `-run` pattern to `'Governance|ROPA|DPIA'`.
7. Update `docs/privacy-data-map.md` with a short factual section describing the new registry (no
   legal claim) and update `plans/fidibeki.plan.md`'s #63 entry.
8. Run `go test ./internal/privacy/...`, `go vet ./...`, `make format-check`, `make governance-check`,
   `git diff --check`.

## Acceptance

- [x] Every one of the 27 retention categories is covered by exactly one processing activity.
- [x] Every activity's `Role` matches the retention registry's role for each of its member
      categories; a mismatch fails validation.
- [x] `RecipientProviders` is derived, not hand-authored, and cannot list a provider absent from the
      provider governance registry.
- [x] Every activity starts `LawfulBasis: pending` and `DPIA.Outcome: pending`; none claims
      `PublicationReady: true`.
- [x] A `required`/`not_required` DPIA outcome or an `identified` lawful basis without complete,
      current, non-placeholder evidence fails validation, mirroring the provider governance registry.
- [x] `make governance-check` covers the new tests and passes.
- [x] Backend tests, `go vet`, `make format-check`, and `git diff --check` pass locally.
- [x] No HTTP surface, migration, or production/deployment change is introduced.

## Post-Review Corrections (same day)

Primary-agent review found and this slice fixed: the ninth EDPB risk criterion mislabeled as a
transfer criterion (renamed to `DPIARiskPreventsRightsOrServiceUse`); "Article 28 basis" language
wrongly implying Article 28 is a lawful basis (reworded); missing `third_parties` data-subject class
and several under-covered activities (`feedback_workflow_management`, `attachment_evidence_handling`,
`ai_assisted_triage`, `integration_notification_delivery`, `privacy_rights_operations`,
`breach_response_recordkeeping`, `external_communications_delivery`,
`infrastructure_and_platform_operations`); a `required` DPIA outcome that could satisfy
`PublicationReady` despite no completion-evidence model (now only `not_required` can); a
hand-authored literal that could preset `PublicationReady: true` (now rejected by validation); and
lawful-basis review evidence with a timestamp but no named reviewer (added `ReviewedBy`, paired and
validated with `ReviewedAt`). See the design doc's "Post-Review Corrections" section for detail.
