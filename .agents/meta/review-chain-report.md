---
skill: review-chain
version: 1
date: 2026-07-23
status: pending-ci-staging
---

# Review Chain Report

**Artifact**: Layered public-submission abuse protection (#26)
**Rounds**: 2 independent specialist reviews plus resolver verification

## Verdict: FIXED LOCALLY

## Issues Found

| Severity | Location | Problem | Resolution |
|----------|----------|---------|------------|
| critical | migrations `65`/`66` | Editing applied migrations would leave upgraded databases without the new lifecycle contract. | Restored the committed files exactly and added forward migrations `68`/`69` with a real version-67 upgrade test. |
| major | upload lifecycle | Terminal expiry/rejection could precede object cleanup and allow replay redeclaration races. | Added retryable `cleaning`, retained verification ownership through presigned-URL expiry, and sealed redeclaration until cleanup. |
| major | attachment/replay verification | Signature-marker checks accepted structurally forged media. | Added bounded structural JPEG, MP3, MP4, WebM, RIFF, Ogg, PNG, gzip, JSON, and rrweb validation with malformed/polyglot coverage. |
| major | verification promotion | Reusable presigned PUTs could overwrite content after verification. | Promote verified bytes to server-owned keys; downstream consumers use only the promoted key. Keys are unique per generation and claim attempt. |
| major | replay finalization | A concurrent batch declaration could race finalization. | Persist finalization intent, lock the session, seal declarations, and finalize only after every batch is safe. |
| major | replay merge cleanup | Merge could delete the batch row while its upload URL was still valid, orphaning recreated staging data. | Keep the row until URL expiry, reconcile staging cleanup, then remove the merged batch manifest; recovery covers interruption between those steps. |
| major | worker performance | Verification buffered large uploads in memory and storage deletion ran inside API transactions. | Stage bounded streams in temporary files, isolate media concurrency, and move object deletion into reconcilers outside request transactions. |
| major | AI maintenance | Daily usage cleanup lacked a leading date index. | Added `(usage_date, project_id)` in migration `69`. |
| minor | abuse diagnostics | Buffered aggregation lost the first occurrence timestamp. | Preserve minimum and maximum timestamps and flush both atomically. |

## Simplifications Applied

The implementation reuses the existing Redis GCRA limiter, tenant transactions, Asynq queues,
storage client, moderation flow, AI generation call, diagnostics ledger, Settings UI, and widget
error envelope. It adds no personal fingerprinting, second AI call, production path, or parallel
upload service.

## Verification

- Migration replay and a real `67 -> 69 -> 67 -> 69` path pass.
- Full backend tests and the exact 80% internal coverage gate pass.
- `gofmt`, `go vet`, and `govulncheck` pass with no called vulnerability.
- Widget typecheck, 117 unit tests, build, and 35 browser journeys pass.
- Web typecheck, 79 unit tests, build, and the CI-concurrency E2E matrix pass with 129 tests and
  45 intentional skips.
- CI and one real staging journey remain before issue closure.
