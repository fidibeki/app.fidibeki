# Rate-limit failure modes

Rate-limit rejection and rate-limiter unavailability are different operational events. A
rejection means the limiter is healthy and a caller exhausted a budget. Unavailability means
the application could not make the distributed decision.

| Surface | Limiter unavailable | Caller response | Side effects |
| --- | --- | --- | --- |
| Public feedback submission | Fail closed | `503` with `Retry-After` | No feedback, outbox event, attachment row, or upload grant |
| Attachment grant/byte budget | Fail closed, text-only degradation allowed | Feedback can return `201` with an attachment warning | Feedback persists; attachments and upload grants are omitted |
| Attachment completion | Fail closed | `503` with `Retry-After` | No completion or verification task |
| Replay start/batch/byte budget | Fail closed | `503` with `Retry-After` | No replay state or object declaration |
| API-key quota | Fail closed | `503` with `Retry-After` | Tenant handler is not called |
| Widget analytics ingestion | Fail closed | `503` with `Retry-After` | No transaction or event insert |

Widget analytics is lower-cost and bounded per batch, but it remains public write ingestion and
must fail closed when the distributed limiter cannot make a decision. API-key quota details are in
[`api-key-quotas.md`](api-key-quotas.md).

## Replay budgets

Replay capture begins after consent for an eligible rule, without requiring a feedback
submission. Page changes can produce batches while the widget is closed. The recorder flushes
nonempty batches every 15 seconds or after 50 events, and stops after five minutes without
pointer, keyboard, scroll, or touch activity.

Batch declarations and completions have separate hourly budgets, each allowing 1000 requests
per network and session and 2000 per project. This accommodates the existing 1000-batch session
cap without charging a declaration twice. Byte budgets retain their 128 MiB session, 512 MiB
network, and 4 GiB project ceilings, with each batch rounded up to KiB. Charging small batches
as whole MiB previously exhausted the session byte budget after just 128 batches.

The widget stops capture and discards queued uploads on `429` or other permanent HTTP
rejections. It retries transient failures only at the failed declaration, upload, or completion
step. An upload failure does not repeat a successful declaration or reserve its bytes again.

The dashboard's Rate limits total counts rejected requests over the selected period, including
replay requests and their retries. It is not a count of visitors or an indication of current
traffic. Historical rejections remain visible until they leave that period.

## Observability

Normal public-feedback quota exhaustion returns `429` and records the hourly abuse diagnostic
reason `submission_rate_limited`. Limiter failure returns `503`, emits the structured warning
`abuse policy unavailable`, and records `feedback-submission_unavailable`. The warning carries
`policy`, `reason`, and `failure_mode`; it never returns the dependency error to the caller.

Alert when the structured warning appears in two consecutive five-minute windows; use the hourly
diagnostic as the durable incident audit. Page immediately if unavailable events affect more than
one project, because that indicates a shared limiter incident rather than a tenant-specific traffic
spike. Do not alert on `submission_rate_limited` alone; investigate it as caller abuse or a
limit-tuning question.

Widget analytics quota exhaustion remains a `429`. Limiter failure returns `503` and emits
`widget analytics rate limiter unavailable` with reason `widget-analytics_unavailable` and
`failure_mode=closed`. Alert on that structured warning using the same consecutive-window and
cross-project thresholds.
