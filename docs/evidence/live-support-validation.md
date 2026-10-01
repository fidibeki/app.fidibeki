# Live support launch validation

Date: 2026-10-01. Branch: `feat/live-support`. Base: `f9ced0e`.
All accounts, workspaces and conversation text in browser evidence are synthetic.

| Check | Result |
|---|---|
| Isolated real-stack browser journey | Passed through production app/backoffice proxies, real Go API, PostgreSQL, Redis and widget assets |
| Staff-only initiation | Empty member thread has no composer; direct member start rejected; staff starts and member replies |
| Delivery and recovery | Incoming preview, unread synchronization, multiple tabs, offline recovery and page reload passed |
| Transcript export | Complete three-message JSON downloaded in browser; paged completeness, cutoff, gaps, cancellation, timeout and byte limits covered by unit tests |
| Feedback | Existing widget mounted inline without a second launcher; browser submission confirmed in PostgreSQL |
| Backend security and data | PostgreSQL/RLS authorization, API-key denial, read-only staff, suspension, required two-factor enrollment, permission/version revocation, idempotency, concurrency, pagination, read markers, stream cleanup, presence expiry, deletion cascade and retention holds passed |
| Operator archive privacy | Real member archive excludes seeded private support content |
| Migration | Up/down/up, full historical migration replay and constraints passed |
| App unit suite | 197 passed, including 14 support cases |
| Backoffice unit suite | 9 passed |
| Widget unit suite | 370 passed, 86.52% statement coverage |
| App browser regressions | 37 passed; one existing desktop-only assertion skipped on mobile |
| Backoffice browser regressions | 4 passed |
| Widget Chromium regressions | 72 passed |
| Type checks and production bundles | App, backoffice and widget passed |
| Docker images | API, app and backoffice passed |
| Static checks | Formatting, go vet, privacy governance, Compose loopback security and whitespace checks passed |
| Full backend suite, coverage and linter | All packages passed; internal statement coverage 80.02%, support 79.3%, response wrappers 94.3%; package floors passed; golangci-lint zero issues |
| Independent review | Two rounds; findings resolved, see review report |
| Responsive review | Desktop and 390px mobile images inspected; header stacking, existing button styling, focus and resize scroll behavior verified |

## Screenshots

- [App desktop](live-support-app-desktop.png)
- [App mobile](live-support-app-mobile.png)
- [Backoffice desktop](live-support-backoffice-desktop.png)
- [Backoffice mobile](live-support-backoffice-mobile.png)

The full backend test run passed every package. Its initial aggregate coverage
was fractionally below the 80% gate. Three focused response-wrapper tests then
verified ordinary-response buffering, immediate streaming chunks, write deadlines
and disconnect errors. The response/middleware packages were rerun with coverage;
their fresh records replaced only those packages in the full-run profile. The
unchanged coverage gate passed that combined current profile.

## Reproduction and limits

Run `pnpm test:live-support` from the repository root, then the checks in
[the release runbook](../live-support.md). The runner creates a unique Docker
project and requires a dedicated loopback fixture database; it cannot fall back
to the ordinary development database. Cleanup deletes only generated fixture IDs.

The review report is at `.agents/meta/review-chain-report.md`. All checks were
local. Production deployment, smoke testing through the production edge, and a
capacity benchmark were not performed. The launch runbook includes deployment
order, required permissions, retention health and a rollback that preserves data.
