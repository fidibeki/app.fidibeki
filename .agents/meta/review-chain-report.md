---
skill: review-chain
version: 1
date: 2026-10-01
status: final
---

# Live support review

Artifact: first-party presence, staff-initiated conversations and the shared app
support launcher. Branch `feat/live-support`, based on `f9ced0e`.
Requirements: `docs/superpowers/specs/2026-10-01-live-support-design.md`.
Two review rounds, with independent security, performance and correctness agents.

## Verdict: FIXED

All findings were resolved. Performance and correctness reviewers returned PASS
in round two. The security reviewer approved the application corrections and
identified two test-fixture isolation issues; both received the requested narrow
fixes, verified locally. No third review round was used.

| Finding | Severity | Confidence | Resolution |
|---|---|---|---|
| Operator-managed member exports could disclose private support transcripts | Major | 10/10 | Removed private support from member/workspace archives; participant-only paged download with completeness, cutoff, cancellation and size checks. Real archive integration proves exclusion. |
| Presence heartbeats refreshed every staff inbox and selected history | Major | 9/10 | Separate Redis/SSE presence event refreshes only the online list. |
| Reconnect could leave an inaccessible history gap, including when send completed before catch-up | Major | 10/10 | Validate adjacency after merging; replace disconnected windows with authoritative pageable history. Reject stale, disconnected older pages. Regression tests cover both cases. |
| Older cached inbox entries retained stale unread counts or deleted conversations | Minor | 10/10 | Authoritative inbox refresh replaces its first page and resets pagination. |
| Redis subscription delay could extend streams past token expiry | Minor | 10/10 | Absolute context/write deadline, pre-send expiry checks and cleanup through response wrappers. |
| Member export query lacked a participant-leading message index | Minor | 10/10 | Removed that query with the private archive fix; download reuses indexed conversation history. |
| Inherited Compose project name could target development services | Major | 10/10 | Unique explicit project argument for startup and cleanup. Verified with an inherited conflicting COMPOSE_PROJECT_NAME. |
| Browser fixture defaulted to development database and deleted a workspace by slug | Major | 10/10 | Explicit isolated-run marker and dedicated loopback port 54339 database required; no deletion by slug; cleanup uses generated IDs only. |
| App panel stacked below the topbar, and initial mobile evidence captured a transition | Minor | 9/10 | Panel at z-index90, above topbar80 and below mobile navigation100/110. Final screenshots disable transitions. Existing app button styling and resize scroll anchoring applied. |

## Validation

- Real-stack browser journey passes against production Go/Nuxt/widget bundles,
  PostgreSQL and Redis: staff initiation, incoming preview, reply, reload,
  disconnect recovery, multiple tabs, transcript download, actual feedback
  submission, Escape/focus return, desktop/mobile overflow checks.
- Real backend integration covers migration up/down/up, RLS, member/tenant
  isolation, machine credentials, staff read-only permissions, revoked access,
  suspension and two-factor policy, ordering, idempotency, read positions,
  pagination, live streaming, presence, cascading deletion and retention holds.
- Operator member export integration confirms support content is absent.
- Frontend: 197 app unit tests, 9 backoffice unit tests, 370 widget unit tests;
  37 relevant app browser tests passed with 1 existing desktop-only skip on mobile,
  4 backoffice browser tests, 72 widget Chromium browser tests. Support has 14
  targeted unit cases. All three frontend type checks pass.
- App, backoffice and widget production builds pass. App and backoffice Docker
  builds include shared source and pass. API image passes; all backend packages pass. Internal coverage is 80.0%,
  with support at 79.3% and response wrappers at 94.3%. Final gate recorded in `docs/evidence/live-support-validation.md`.
- Formatting, go vet, golangci-lint with zero findings, privacy governance checks, Compose security checks and
  git diff whitespace checks pass.
- Independent Impeccable detector returned no findings. Four responsive images
  live at `docs/evidence/live-support-*.png`; final images dated 18:01 include
  the corrected app button and scroll behavior.

## Input quality

| Input | Rating | Evidence |
|---|---|---|
| Product context | Rich | Existing app, backoffice, auth, privacy and widget contracts inspected. |
| Requirements | Precise | Explicit staff-only initiation and launch acceptance spec. |
| Artifacts | Fresh | Current source and spec dated 2026-10-01. |

## Simplifications and limits

Transcript download reuses the scoped member history API, avoiding a new export
job system. Inbox invalidation resets older cached pages instead of maintaining
an inconsistent partial cache. No findings were declined. The final linter also required explicitly ignoring
the existing development seed connection-close error, a one-line cleanup. Tests are local;
production edge verification remains a release step. No load benchmark or
production deployment was performed during the review. The subsequent release
passed full CI and production smoke testing; evidence is in
`docs/evidence/live-support-production.md`. The rollout and data-preserving
rollback procedure is in `docs/live-support.md`.
