---
skill: review-chain
version: 1
date: 2026-10-01
status: final
---

# Tenant website chat review

Artifact: `feat/tenant-live-chat`, tenant dashboard conversations and the website chat/feedback embed, migration 139 and the isolated integration/browser tests.
Requirements: [tenant website chat design](../../docs/superpowers/specs/2026-10-01-tenant-website-chat-design.md). The tenant contacts visitors to its own website; visitors can reply only after the tenant initiates.

Two independent security, correctness and performance review rounds, with a separate read-only resolver. No third round was used.

## Verdict: FIXED

Eight confirmed findings were resolved. Correctness and performance returned PASS in round two. Security confirmed its original permission finding resolved, then identified one remaining offline-reset case. A durable reset marker and the requested navigation regression test resolve that final finding.

| Finding | Severity | Confidence | Resolution |
|---|---|---|---|
| Host logout reset was a no-op before chat mounted | Major | 9/10 | Loader-level reset invalidates pending mounts, clears project credentials independently of the view, revokes captured credentials and prevents stale initialization reconnecting. Browser regression resets on `fidibeki:ready`. |
| A six-permission subset could silently grant chat access to a custom role | Major | 9/10 | Migration requires the entire prior administrator permission set and increments authorization version. Migration test verifies full administrators and two partial roles. |
| Slow request bodies could hold the shared database pool | Major | 9/10 | Read at most 20,001 bytes before authorization; reject over 20,000. A streaming-body regression verifies no database acquisition before input completes. |
| Retention capped total cleanup at 1,000 visitors per day | Major | 10/10 | Full bounded sweeps resume after one second. Test drains 2,200 records over three sweeps and checks failure health reporting. |
| Changing selection during older-history loading left the new thread disabled | Minor | 10/10 | Reset selection-owned flags and reject old asynchronous results. Desktop/mobile browser tests switch visitors with the old request pending. |
| Refresh discarded loaded inbox pages and raced pagination | Minor | 10/10 | One shared refresh promise retrieves the user-requested page window and atomically replaces it. The browser regression preserves 52 rows through refresh. |
| Chat feedback reserved an automatic rule permanently | Minor | 9/10 | Internal mounts no longer reserve automatic rules; host explicit mounts retain that behavior. Browser regression opens feedback, disables chat and verifies automatic feedback returns. |
| Configuration failure during unmounted reset could restore a prior transcript after navigation | Major | 9/10 | Persist a public-key-scoped reset marker synchronously; consume it before reading any credential on later loads. Marker expires after the credential's maximum 24-hour lifetime. Browser regression simulates configuration failure, reset, navigation and recovery and rejects old history credentials. |

## Input quality

Product context was initially wrong: the preceding feature addressed first-party platform support. The user's correction explicitly requires the B2B tenant-to-own-website boundary. The new same-day design records that boundary, reply-only behavior, permissions, anonymous identity and retention. Requirements are precise for this release; upstream artifacts are fresh. Existing platform support is separate and covered by the same real-stack regression suite.

## Resolution notes

No findings declined. Existing full administrators receive permissions; custom roles, including any customized owner role, remain unchanged. The fixes preserve row-level tenant/project/visitor isolation, credential expiry, bounded streams, message ordering/idempotency, and legal holds. The browser credential/reset marker is also included in the executable privacy inventory.

See [Website conversations](../../docs/website-chat.md) for product behavior and host integration. Validation evidence is recorded with the feature's release record.
