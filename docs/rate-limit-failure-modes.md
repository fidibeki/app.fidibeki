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
