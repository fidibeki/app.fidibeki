# Fidibeki Security Baseline Implementation Plan

**Design:** `docs/superpowers/specs/2026-07-22-fidibeki-security-baseline-design.md`

## Goal

Remove the audited Go vulnerabilities and establish reproducible vulnerability and secret-scanning
gates without changing production infrastructure.

## Tasks

1. **Record the failing security baseline**
   - Run pinned `govulncheck v1.6.0` against `backend/...` and retain the advisory identifiers in the
     command output, not in a tracked report.
   - Run Gitleaks CLI `8.30.1` over full history with redaction and a temporary JSON report outside
     the repository.
   - Confirm every existing Gitleaks finding is one of the reviewed deterministic example or test
     fixtures before creating the baseline.

2. **Patch the backend dependency graph**
   - Update `backend/go.mod` and `backend/go.sum` to the exact fixed module versions in the design.
   - Run `go mod tidy`, compile the full backend, and adapt only call sites affected by verified API
     incompatibilities.
   - Re-run pinned `govulncheck`; require zero reachable vulnerabilities.
   - Run `make format-check`, `make lint`, `make test-short`, and `make coverage`.
   - Commit the dependency remediation independently as `fix(deps): patch backend vulnerabilities`.

3. **Add the local vulnerability command**
   - Add `vulncheck` to `Makefile` and `.PHONY` using the pinned analyzer command from the design.
   - Verify the Make target from the repository root and confirm it fails when a temporary reachable
     vulnerable call is introduced, then remove that untracked test mutation.

4. **Create the exact secret baseline**
   - Add root `gitleaks.toml` with the built-in rule set enabled.
   - Add root `.gitleaksignore` with only exact fingerprints from the reviewed full-history report;
     group comments by fixture purpose and include no secret values.
   - Re-run the redacted full-history scan and require success.
   - Add a temporary synthetic secret in an untracked test commit/worktree state, prove Gitleaks
     fails, and remove it before committing.

5. **Add Security CI**
   - Create `.github/workflows/security-ci.yml` for pushes to `main`, pull requests, and manual runs.
   - Add independent `govulncheck` and `gitleaks` jobs with read-only permissions.
   - Pin the Gitleaks action commit and CLI version specified in the design, fetch full history, and
     disable comments and artifact upload.
   - Check workflow syntax and run both underlying commands locally.
   - Commit the Make target, baseline, and workflow as `ci: enforce repository security baseline`.

6. **Enable repository security settings**
   - Enable vulnerability alerts through the GitHub repository API and verify the endpoint succeeds.
   - Enable automated security fixes and verify their status.
   - If the private-repository account rejects either feature, capture the exact response in the
     verification summary; do not weaken CI.

7. **Review and publish the security change**
   - Run the security-sensitive review chain against all dependency, scanner, and workflow changes.
   - Resolve every correctness or security finding and repeat affected checks.
   - Push the documentation and implementation commits to `main`.
   - Require green Backend CI, Web CI, Release Artifacts, and Security CI; manually dispatch Security
     CI once to prove the full-history path.

8. **Verify staging only**
   - Wait for Railway staging API and worker deployments affected by the backend graph.
   - Confirm API readiness plus web and widget health endpoints.
   - Inspect API and worker startup/error logs.
   - Complete the controlled staging authentication and feedback-submission smoke.
   - Do not promote or modify production.
