# Breach Documentation Exports Implementation Plan

> **Scope mode:** LOCKED
>
> **Design:** `docs/superpowers/specs/2026-07-24-breach-documentation-exports-design.md`
>
> **Boundary:** staging-only product work. No migration, deployment, production change, resent
> notification, or mutated incident.

## Task 1: Domain Rendering

Add `exports.go`: `ExportFormat` (`json`/`markdown`) with `ParseExportFormat`; wrapper types for the
notification export and the post-incident report; a pure `actionHistorySummary` rollup over
`[]ActionEvent`; JSON marshaling helpers that normalize nil slices to empty ones; and deterministic
Markdown renderers for both documents (ordered-slice/struct traversal only, no maps). Unit-test
format parsing, the action-history rollup, an explicit-`null`/pending `latest_assessment`, nil-slice
normalization, and that both renderers are deterministic and free of any forbidden-field keys
(mirroring `TestIncidentJSONNeverExposesTenantContent`'s shape-guard style).

## Task 2: HTTP Routes

Add `exports_http.go`: two GET+HEAD routes reusing `BreachStore.GetIncident` (no new store method),
`platform:breach:read`, safe `Content-Type`/`Content-Disposition`/`Cache-Control: private,
no-store`/`X-Content-Type-Options: nosniff`/`Referrer-Policy: no-referrer` headers, and an
`HTTPOptions.Now` clock (defaulting to `time.Now`) for `generated_at`. HTTP tests cover
audience/permission denial, malformed/unknown incident and notification IDs, invalid/missing
`format`, exact frozen notification content against a fake store fixture, explicit pending values in
the report, both formats for both documents, and `HEAD` returning identical headers with an empty
body.

## Task 3: Reconcile Product Records And Run Gates

Update `docs/privacy-data-map.md`'s breach-register row to mention the read-only export surface and
`plans/fidibeki.plan.md`'s #64 entry with a new slice bullet, without claiming #64 complete. Run
`go test ./internal/breachregister -count=1`, `go test ./... -count=1`, `go vet ./...`,
`go build ./...`, `make format-check`, `make governance-check`, and `git diff --check`. Commit the
design/plan docs separately before implementation, then commit the verified vertical slice. Do not
deploy, push, or perform a staging call.
