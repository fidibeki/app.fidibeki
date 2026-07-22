# Fidibeki Security Baseline Design

**Date:** 2026-07-22
**Status:** Approved
**Target:** Repository and staging validation only; production remains deferred

## Problem

The application passes its functional staging checks, but the repository has no automated dependency
vulnerability or secret-scanning gate. A repository-wide audit found four reachable Go
vulnerabilities and additional fixable package-level advisories. A full-history Gitleaks scan found
only deterministic example and test fixtures, but those known false positives currently prevent a
clean secret-scanning baseline.

The baseline must remove the known dependency exposure, make regressions fail CI, and preserve a
reviewable record of the existing false positives without weakening detection for future changes.

## Goals

- Upgrade the affected Go modules to versions that contain the published fixes.
- Run the official Go vulnerability analyzer as a repeatable local command and CI check on every
  push and pull request.
- Run Gitleaks for every push and pull request, with an explicit full-history audit path.
- Suppress only the exact, reviewed fixture findings already present in history.
- Enable GitHub Dependabot vulnerability alerts and automated security fixes when the repository
  account supports them.
- Verify the changes locally, in GitHub Actions, and on staging without touching production.

## Non-goals

- No production deployment, production configuration, or production credential work.
- No SBOM generation, container-image scanner, DAST suite, or broad AppSec platform rollout.
- No dependency modernization beyond versions needed to clear the current advisories and their
  transitive graph.
- No history rewrite and no rotation of values proven to be deterministic fixtures rather than live
  credentials.
- No broad path, rule, or entropy exclusions in Gitleaks.

## Dependency Remediation

Update the backend dependency graph to at least these exact versions:

- `github.com/go-chi/chi/v5 v5.2.4`
- `github.com/jackc/pgx/v5 v5.9.2`
- `github.com/testcontainers/testcontainers-go v0.43.0`
- `github.com/testcontainers/testcontainers-go/modules/postgres v0.43.0`
- `golang.org/x/net v0.56.0`
- `golang.org/x/text v0.39.0`
- `golang.org/x/crypto v0.53.0`
- `google.golang.org/grpc v1.82.1`

The first four are direct dependencies. The remaining modules may stay indirect, but they are pinned
explicitly because the audited graph otherwise resolves to vulnerable versions. `x/crypto v0.53.0`
is required by `x/net v0.56.0` and supersedes the independently safe `v0.52.0` audit floor. Enabling
Dependabot surfaced a high-severity gRPC advisory published on 2026-07-21 and advisories against the
legacy Docker module used by Testcontainers. `grpc v1.82.1` includes the new fix, while Testcontainers
`v0.43.0` uses the split Moby API/client modules instead of `github.com/docker/docker`. The legacy
module remains in module metadata through the upstream migrator driver's own test dependencies; it
is absent from the application package graph and must be triaged in Dependabot as `not_used` with
that evidence rather than suppressed in code.

Dependency changes are produced with Go module tooling, followed by `go mod tidy`; `go.mod` and
`go.sum` remain the only dependency source of truth.

The implementation must not hide a vulnerability with an analyzer ignore. If a listed version is
incompatible with the application, the change stops for a code-level compatibility fix or a newly
reviewed version; it does not restore the vulnerable version.

## Vulnerability Gate

Add `make vulncheck`, running:

```text
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
```

from `backend/`. Pinning the analyzer makes local and CI results reproducible while its vulnerability
database remains current. A dedicated `Security CI` workflow runs the same Make target on pushes to
`main`, pull requests, and manual dispatches. A reachable vulnerability or analyzer failure is a hard
failure. Package-level advisories that are not reachable remain visible in verbose audit output but
do not fail the standard symbol-level gate.

The workflow uses the Go toolchain declared in `backend/go.mod` and the existing module cache pattern.
It has read-only repository permissions. GitHub's current private-repository plan does not support
branch protection or rulesets, so the checks cannot technically block direct pushes to `main`. The
workflow remains fail-closed and visible on every commit; making it a merge-required check requires
GitHub Pro or a public repository and is deferred as an external account limitation.

## Secret-Scanning Gate

The same workflow contains a separate Gitleaks job so Go setup or dependency failures cannot prevent
secret scanning. It checks out and scans full history on every event. It uses:

- The official Gitleaks `v8.30.1` container pinned to immutable digest
  `sha256:c00b6bd0aeb3071cbcb79009cb16a60dd9e0a7c60e2be9ab65d25e6bc8abbb7f`.
- A direct CLI invocation with redaction and no GitHub token, PR API calls, comments, or report
  artifact.
- `actions/checkout` and `actions/setup-go` pinned to their reviewed release commit SHAs.

Scanning the complete repository history avoids merge-diff, first-parent, pagination, and fork-PR
coverage gaps. The repository is small enough that the stronger scan takes less than one second
locally. The initial implementation must run the same full-history command locally and then run the
manual CI path once after merge.

Add a root `gitleaks.toml` that extends the built-in rules without weakening them. Add a root
`.gitleaksignore` containing only exact Gitleaks fingerprints for the 17 findings already reviewed as
deterministic examples or test fixtures. Each baseline group is documented by fixture purpose. A new
line, commit, file, or rule produces a different fingerprint and must fail the gate. Future false
positives require individual review and exact fingerprint additions in the same pull request.

No report may print unredacted secret material. Local validation uses redacted output and stores any
temporary report outside tracked files.

## Dependabot

Enable repository vulnerability alerts first, then automated security fixes through GitHub's
repository security API, and verify both settings. This does not add routine version-update pull
requests or alter merge policy.

The current private-repository account may reject these settings. That rejection is not a reason to
weaken the CI gates: record the exact GitHub limitation in the implementation result and leave the
locally enforceable vulnerability and secret checks active.

## Failure And Recovery

- A dependency upgrade breaks compilation or tests: keep the fixed version and adapt the affected
  call site; do not suppress the advisory.
- `govulncheck` cannot download its advisory database: fail CI and retry as an infrastructure issue.
- Gitleaks finds a possible secret: fail CI, inspect the redacted finding, rotate and remove a real
  credential, or baseline only an independently verified fixture fingerprint.
- A Gitleaks action or CLI download fails: fail CI rather than skipping the scan.
- Dependabot enablement is unavailable: preserve the API error in the verification record and rely on
  the repository-owned checks until account support changes.

## Testing And Verification

Local verification:

- `go mod tidy` produces no unexpected dependency drift.
- `make format-check`, `make lint`, `make test-short`, and `make coverage` pass.
- `make vulncheck` reports no reachable vulnerabilities.
- The redacted full-history Gitleaks scan passes with the exact baseline.
- A temporary synthetic secret and a deliberately vulnerable test fixture each prove their
  corresponding gate fails; neither fixture is committed.

GitHub verification:

- `Backend CI`, `Web CI`, existing release checks, and the new `Security CI` succeed on `main`.
- A manual `Security CI` run completes the full-history scan.
- Dependabot alerts and automated security fixes report enabled, or the account limitation is
  documented precisely.

Staging verification:

- Deploy only the services affected by the dependency graph.
- Confirm API readiness and web/widget health endpoints.
- Inspect API and worker logs for startup, database, queue, and request failures.
- Run the existing controlled staging smoke for authentication and feedback submission.

Production remains unchanged throughout this work.
