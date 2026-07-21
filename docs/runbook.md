# Operations runbook

Start every incident by recording the UTC start time, affected environment, last healthy deploy,
request IDs, and tenant IDs. Do not paste access tokens, magic links, webhook payload secrets, or
customer evidence into chat or tickets.

## 1. Database migration failure

1. Keep the failed deploy inactive; Railway pre-deploy failure leaves the previous instance live.
2. Inspect migration logs and `fidibeki-api migrate version` using `DATABASE_URL_DIRECT`.
3. If the schema is dirty, stop deploys and take a database backup before changing anything.
4. Fix forward with a new migration. Use `migrate down` only when that exact down migration was
   rehearsed against a restored production snapshot.
5. Run up-down-up CI, deploy staging, check `/readyz`, then promote.

## 2. Stripe webhook replay

1. Confirm the endpoint secret and event ID in Stripe; never replay an edited payload.
2. Check `stripe_events` for the event ID and its processing result.
3. If it succeeded, do not replay: processing is idempotent and the problem is downstream state.
4. If it failed before persistence, replay from Stripe Workbench and follow logs by request ID.
5. Verify tenant plan, status, subscription ID, and pending plan against Stripe after completion.

## 3. Redis queue backlog

1. Check Redis memory, connectivity, and worker restarts before changing concurrency.
2. Sign in with a platform-admin magic link and inspect queue depth and oldest task at
   `/admin/queues/` for `email`, `ai`, `replay`, and `integration` pools.
3. Scale only the affected worker pool and respect Gemini and provider rate limits.
4. Preserve failed tasks for diagnosis; retry transient failures and archive malformed permanent jobs.
5. Alert if queue age continues rising for 15 minutes or Redis persistence reports errors.

## 4. AI processing failures

1. Filter feedback by failed AI status and inspect attachment error class and attempt count.
2. Check Gemini quota, model availability, Redis limiter state, and object accessibility.
3. Retry one representative attachment before bulk reprocessing.
4. For content-policy or malformed-media failures, leave the permanent failure visible to users.
5. After recovery, use Reprocess all in Dispatch and confirm `ai_processed_at` is populated.

## 5. Email delivery failures

1. Check Resend status and `delivery_logs` for provider response, attempt number, bounce, or complaint.
2. Confirm `RESEND_AUTH_FROM`, domain verification, API key, and webhook signature configuration.
3. Do not resend complaints or hard bounces; those addresses must remain unsubscribed.
4. Retry transient provider errors through the email queue and verify a new delivery-log attempt.
5. Use the capture driver only in local/test environments.

## 6. Tenant suspended incorrectly

1. Compare tenant status, trial end, Stripe subscription, latest Stripe events, and deletion request.
2. Confirm the request resolves the expected tenant slug; do not bypass tenant isolation middleware.
3. Correct source-of-truth billing state, then replay the relevant Stripe event or apply the audited
   platform-admin correction.
4. Verify `/v1/config`, tenant login, and one widget config request before closing the incident.
5. Record who changed the status and why.

## 7. Widget not loading

1. Fetch `/v1/loader.js` and confirm a short cache policy, then fetch one manifest asset and confirm
   `Cache-Control: public, max-age=31536000, immutable`.
2. Check the script `data-fidibeki-key`, request Origin, project allowed domains, and active rule.
3. Verify `/v1/widget/config` through the widget host and inspect CORS and CSP console errors.
4. Test in a clean browser session because trigger suppression is stored in `sessionStorage`.
5. Roll back the widget edge service if stable entry shims reference missing hashed assets.

## 8. S3 storage full or unavailable

1. Check `/readyz` storage status and provider capacity, latency, credentials, and bucket policy.
2. Pause media/replay-heavy ingestion if uploads are failing; text-only feedback remains preferable to
   accepting evidence declarations that cannot complete.
3. Do not delete database rows before their objects. Run retention cleanup only after storage recovers.
4. Verify presigned upload, HEAD completion, authenticated attachment stream, and replay stream.
5. Increase capacity or lifecycle coverage, then reconcile pending attachments and replay merges.

## 9. Deploy rollback

1. Stop new promotions and identify whether the failure is API, worker, web, or widget edge.
2. Redeploy the last healthy Railway deployment for only the affected service.
3. Never roll application code behind an irreversible schema change; deploy a compatible forward fix.
4. Check `/readyz`, staging smoke, login, feedback list, and one widget config request.
5. Keep the failed deployment logs and Sentry release for the post-incident review.

## 10. Secrets rotation

1. Create the replacement in the provider first and record its owner and expiry.
2. Update staging, deploy, and verify the affected integration before production.
3. For `JWT_SECRET`, expect all sessions to be revoked. For `INTEGRATION_ENCRYPTION_KEY`, re-encrypt
   stored integration credentials before removing the previous key.
4. Deploy production through the approval workflow, verify, then revoke the old provider credential.
5. Search logs and repository history for accidental exposure and rotate again if exposure is possible.
