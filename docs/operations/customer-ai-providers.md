# Customer AI accounts

Customers connect API credentials billed to their provider account. Consumer subscriptions such as ChatGPT Plus and Claude Pro do not supply API access. Existing widgets retain managed Gemini until an administrator changes the widget's AI consent contract.

## Supported profiles

| Connection | API | Inputs |
| --- | --- | --- |
| Managed or customer Google | Gemini Developer API | Text, screenshots, audio, video |
| OpenAI | Chat Completions with strict JSON schema | Text, verified screenshots |
| Anthropic | Messages with structured JSON output | Text, verified screenshots |
| Amazon Bedrock | Converse with forced structured tool output | Text, verified screenshots |
| Compatible service | OpenAI Chat Completions or Anthropic Messages profile above | Text, verified screenshots |

Compatibility is a tested subset. Listing a model does not validate it. Validation invokes the exact model with synthetic content and verifies the intended input types; models without the required structured output or image capability fail. Manual IDs work when discovery is unavailable. Bedrock accepts model IDs or accessible inference profile IDs/ARNs. Native non-Gemini audio/video, Azure-specific authentication, Vertex IAM and private/local endpoints are outside this release.

## Deployment

1. Apply migration 143 with customer flags empty. Retain previous encryption keys.
2. Deploy the API, schedulers and **every** worker before enabling customer accounts. New inference uses `ai-pool-v2`; new workers also drain `ai-pool`. Queue separation does not make old scheduling/maintenance processes safe for customer work.
3. Set `AI_ENCRYPTION_KEYS` to a JSON map of key IDs to base64 32-byte AES keys and `AI_ACTIVE_ENCRYPTION_KEY` to the active ID (default `v1`). If omitted, the integration encryption key is used as `integration-v1`; retain this ID when migrating. Never remove a decrypt key while a connection or cleanup envelope refers to it.
4. Set `AI_CUSTOMER_PROVIDERS` to a comma-separated subset of `google_gemini,openai,anthropic,amazon_bedrock,openai_compatible,anthropic_compatible` and `AI_CUSTOMER_TENANTS` to pilot workspace UUIDs, or `*` after pilot acceptance. Both default empty. `AI_MANAGED_MODE=enabled` preserves managed Gemini. `disabled` removes managed inference and its production key requirement.
5. Complete the existing provider-governance review before enabling production recipients. Registry entries remain pending; code does not invent contract or legal approvals. Release updated mobile SDKs before enabling customer providers for strict older mobile clients.
6. Run synthetic live smoke tests using test provider accounts: each profile's text and screenshot output, Gemini upload/delete, expired/revoked keys, throttling, model access, and Bedrock trust restrictions. Protocol fixtures cover these branches locally; deployment verification needs actual accounts. Check charges in the provider account. Usage counts are evidence, not invoices or dollar estimates.

Administrators need `ai_connection:read` and `ai_connection:manage`. Rule editors need read permission to choose customer connections. Migration 143 grants both to owners and existing broadly privileged administrators; custom grants remain explicit.

## Customer workflow

Open Settings → AI providers. Add a connection and credential, discover or enter a model ID, then validate the intended content types. A preferred model and default connection prefill new widget configurations. Choose the account, model, content types and privacy URL in the widget's AI dialog and acknowledge the destination. Saving the widget activates the exact contract; changing the workspace default never rewrites existing widgets.

Credentials are write-only and encrypted with tenant, connection and version authenticated context. Custom API URLs require public HTTPS on port 443. The shared transport rejects private/loopback/metadata IPs, mixed unsafe DNS answers and redirects. Custom authentication supports one API credential. Never log request bodies, credentials or raw provider error bodies.

## Bedrock IAM

Configure a dedicated platform caller through `AI_AWS_ACCESS_KEY_ID`, `AI_AWS_SECRET_ACCESS_KEY`, optional `AI_AWS_SESSION_TOKEN` and exact `AI_AWS_CALLER_ARN`. There is no ambient AWS credential chain. The caller needs STS access to the customer role; inference uses only assumed-role credentials.

The connection displays a generated external ID and the platform caller ARN. The customer role trust policy must restrict Principal to that caller and require `StringEquals` on `sts:ExternalId` with the exact connection external ID. Validation checks caller identity, requires denial with both missing and incorrect IDs, then assumes with the correct ID. Wildcards do not satisfy this check. Grant `bedrock:InvokeModel` for the chosen models/profiles and required regional resources. Listing additionally needs the relevant list permissions; manual model validation does not. Short-lived role credentials are reacquired for each resolution and checked for expiry before signing.

An expiring Bedrock API key is available for pilots. Production onboarding should use IAM roles.

## Rotation and cleanup

Key rotation increments credential version and invalidates model validation. Revalidating the same model permits unchanged contracts to resume. Endpoint, region, role or authentication changes increment destination version and require widget reauthorization. Disabling or deleting a connection prevents new dispatch and stale result persistence. There is no provider/model/managed-key fallback.

Jobs capture the consent contract, provider, connection, destination version and model before enqueue. Workers check eligibility at claim, before outbound calls and at persistence. Already-sent requests cannot be recalled; their results are discarded if authorization changes. Tenant/project daily budgets still apply. Undispatched cancellation refunds its reservation; dispatched work remains counted. Connection RPM and concurrency limits supplement these budgets.

Gemini cleanup retains a separate encrypted credential snapshot, surviving rotation and connection/attachment/workspace deletion. Successful deletion erases this ciphertext. Failed deletion retains the minimal tombstone and cleanup credential for an operator retry; this key cannot be reused for inference. Where possible, finish pending cleanup before revoking a provider key. Monitor `ai_gemini_files` where `deleted_at IS NULL`, especially `failed_at IS NOT NULL`. Resolve access, requeue the failed deletion and preserve evidence. A provider's advertised expiry alone is not a successful deletion confirmation.

## Rollback and evidence

Disable the customer flags to stop dispatch. Keep the new cleanup workers and decrypt keyring deployed until remote cleanup completes. Keep migration 143 applied during rollback; managed-only code is unsafe while customer contracts or jobs exist. The down migration refuses when customer or pending credential-bearing cleanup evidence exists. Do not erase evidence to force a rollback.

Processing exports include requested/resolved models, billing mode, connection/config/credential versions, safe failure classifications, provider attempts and reported tokens. Workspace exports include connection metadata, never credentials or cleanup envelopes. Connection changes and validation appear in the access audit. External provider retention follows the customer's account settings and contract.
