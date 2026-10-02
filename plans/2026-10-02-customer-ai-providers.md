# Customer-owned AI providers

Status: Approved by the user on 2026-10-02. Implemented on feat/customer-ai-providers. Customer rollout remains disabled by default; live provider smoke tests are deployment gates.
Date: 2026-10-02

## Outcome and scope

Customers connect their own API accounts and choose the model used for feedback analysis. The user confirmed API credentials, not consumer subscriptions. Preserve Fidibeki-managed Gemini as the existing default. Add native Google Gemini, OpenAI, Anthropic, Amazon Bedrock, and custom OpenAI-compatible or Anthropic-compatible endpoints. Customer accounts pay their provider directly. Existing Fidibeki plan eligibility and daily processing limits still apply; changing product pricing is outside this change.

Connections belong to a workspace. Each existing AI-enabled widget rule stores a specific connection and model. An optional workspace default pre-fills new configurations; changing it never reroutes existing rules. Multiple connections are allowed. One connection/model handles that rule's enabled inputs in the first release. No automatic provider fallback or silent model replacement.

## What the repository does today

- `backend/internal/ai/gemini.go` exposes an Analyzer interface, but constructs one Gemini client and handles native text, screenshots, audio, video, structured JSON, file upload and deletion.
- `backend/cmd/fidibeki-api/main.go` wires a single environment key/model into the worker. `Processor` uses one analyzer and a shared `gemini` rate-limit key. `Store.Model` and processing-run inserts assume Google.
- `backend/internal/project/models.go`, validation, migration 000083, web types, the AIProcessingDialog, EN/PT copy, and widget types/disclosures restrict or name Google Gemini.
- Existing scheduling, pre-call authorization, completion, tenant lifecycle, redaction, quarantine, daily reservations and provider-file deletion protect customer data. Preserve these invariants.
- Reuse the AES-GCM pattern in `backend/internal/integration/crypto.go` with tenant/connection/version associated data. Reuse `backend/internal/outboundhttp/policy.go`, which validates DNS at dial time, blocks non-public destinations and redirects, and disables environment proxies.
- Update the provider inventory and code release checks in `backend/internal/privacy`, plus privacy exports, erasure and operational cleanup. This is a cross-cutting provider change, not only a settings page.

## Customer flow

1. Workspace settings > AI providers > Add connection. Choose provider, name the connection, and enter the provider-specific credential. Custom endpoints also ask for HTTPS base URL and protocol. Bedrock asks for AWS region and authentication configuration.
2. Validate connection using model discovery and a bounded synthetic analysis request. Never send customer feedback for setup tests. Explain that the test can incur a small provider charge. Show invalid credential, unavailable model, missing permissions, quota, incompatible output and unsupported endpoint separately.
3. Choose a discovered model. Filter by the adapter's verified capabilities. Show exact model ID, supported inputs and validation state. Allow manual IDs where discovery is unavailable, followed by validation. A model-list response is not proof that invocation will work.
4. In the existing rule AI dialog, choose managed Gemini or a workspace connection, select the model and allowed input categories, and acknowledge the actual provider/data destination. A change to provider, endpoint, region, model or categories requires an explicit save with the relevant acknowledgement. Credential-only rotation within the same connection does not change the data-processing contract.
5. Display the selected provider/model, connection health, safe failure reason and processing status. Reconnect, rotate, disable or retry explicitly. Failed BYOK work never moves to Fidibeki's paid Gemini account.

## Provider and media contract

| Connection | Transport | Initial supported inputs | Selection |
| --- | --- | --- | --- |
| Fidibeki-managed Gemini | Existing native Gemini | Existing text, screenshot, audio, video | Existing managed model |
| Customer Google Gemini | Native Gemini SDK | Text, screenshot, audio, video, subject to model capability | Gemini models API + verified capability catalog |
| OpenAI | Chat Completions baseline | Text and screenshots for verified compatible models | Account models + catalog + synthetic validation |
| Anthropic | Messages API | Text and screenshots for verified compatible models | Models API + capability metadata + validation |
| Amazon Bedrock | Native Converse | Text and screenshots for verified Converse models | Region-aware model/profile discovery or manual model/profile ID + validation |
| Custom OpenAI-compatible | Chat Completions | Text initially; screenshots only after validated support | Optional models endpoint or manual ID |
| Custom Anthropic-compatible | Messages | Text initially; screenshots only after validated support | Optional models endpoint or manual ID |

OpenAI format means Chat Completions for this release, not a promise to support every Responses-only endpoint. Anthropic compatibility means the supported Messages subset. Generic compatibility does not establish file, audio, video or schema support. Existing Gemini media continues working. A rule cannot enable an unsupported input; if capabilities later disappear, that input is marked skipped/blocked with a reason. Uploading feedback remains available. Native OpenAI audio transcription, video conversion and per-input multi-provider routing are follow-up work, not implied by this release.

Official AWS documentation now exposes compatible APIs too, but availability varies by model, endpoint and region. A Converse adapter avoids pretending every Bedrock model implements one compatible format. Production authentication uses a customer IAM role with a Fidibeki-generated external ID and temporary STS credentials. Provide a narrow onboarding role template and an explicit Fidibeki AWS caller identity. Bedrock API-key mode can support setup/pilot connections with visible expiry; production acceptance includes role-based auth. Do not accept AWS root credentials or ambient credentials that accidentally bill Fidibeki.

## Backend design

Use a small internal provider layer within the Go backend. Avoid a new external AI proxy service. Separate provider identity, API protocol and billing mode, so a custom endpoint speaking OpenAI is not recorded as OpenAI's service.

- Connection store: workspace-owned connection records, encrypted credential versions, endpoint/region/auth configuration, safe display label, health and disabled state. Composite tenant foreign keys and row-level security enforce ownership. Never return secrets after save; API responses expose only masked metadata. Introduce dedicated connection read/manage permissions and preserve rule-edit permissions for selecting approved connections. Owners/admins can manage by default; other roles require explicit grants. Write non-secret audit events for create, validate, rotate, change destination, disable and delete, including actor and affected rules. Configuration updates use version checks so concurrent edits cannot overwrite each other.
- Resolver/factory: after tenant authorization, resolve the pinned connection/model into a bounded per-tenant client. Cache by tenant, connection, config and credential version, expire entries, and invalidate on disable/rotation. Keep plaintext credentials out of browser storage, Redis payloads, logs, diagnostics and exports.
- Adapter contract: discovery, capability validation, text/media analysis, normalized usage and errors, plus cleanup where needed. Keep common prompts, pre-provider minimization, strict local schema validation and ProtectResult outside individual adapters. Translate provider-specific schema subsets without weakening local validation. Handle refusals, truncation, malformed JSON, oversized bodies, unsupported options and missing fields explicitly. No remote tools or agents.
- Endpoint protection: official endpoints fixed by provider; custom public HTTPS URLs pass the existing guarded outbound client for discovery, validation and inference. Reject URL credentials, query secrets, fragments, redirects, internal IPs and DNS rebinding. Bound request/response sizes, duration and concurrency. No arbitrary header editor in the first release; support standard bearer or x-api-key auth as the selected protocol requires.
- Operational errors: normalized auth, forbidden, model unavailable, unsupported capability, quota, timeout, rate limit, provider unavailable, invalid output and disabled connection codes. Respect Retry-After, bounded retries and jitter. Isolate per-connection limits and preserve the managed Gemini shared limit plus overall worker limits. Let customers lower custom-connection RPM/concurrency under server ceilings; conservative defaults apply even when providers omit rate-limit headers. Bound model-discovery and validation endpoints too. Separate terminal quota exhaustion from transient 429 rate limits.

Use explicit protocol profiles for unsupported optional parameters and schema variations. A manual model ID still needs a synthetic invocation and valid output. A customer's assertion cannot bypass the screenshot capability check or local schema validation. Model discovery is cached per tenant/connection/config/credential version with a short TTL, bounded pagination and explicit refresh. Discovery failure offers validated cached choices or manual ID testing; it never swaps the selected model. Store requested IDs and resolved model/version when the provider reports one. Provider aliases can change upstream, so prefer immutable IDs where available and do not imply stronger pinning than the provider offers.

Connection management uses `GET/POST /v1/ai/connections`, `PATCH/DELETE /v1/ai/connections/{id}`, `POST /v1/ai/connections/{id}/validate`, and `GET /v1/ai/connections/{id}/models`. Add `GET /v1/ai/providers` for supported connection types and safe capabilities. New connections start unvalidated; validation does not enable processing or alter rules. Model-list calls are read-only and bounded. Validation uses synthetic inputs, records status safely and requires manage permission. Rule configuration continues through existing project endpoints. Deletes return a disabled/cleanup-pending state when remote deletion is outstanding. Public SDK responses use a separate safe disclosure DTO rather than the internal rule configuration.

Managed Gemini is a logical system connection with no customer-editable key. Legacy null connection IDs mean managed only when billing mode explicitly says managed; a missing/deleted BYOK connection must fail closed. Remove the unconditional production requirement for `GEMINI_API_KEY` when managed AI is intentionally disabled. BYOK-only workers must start normally, while the managed option clearly reports unavailable.

## Persistence and lifecycle

Add `ai_connections` and `ai_connection_credentials`; version data-destination configuration separately from credential rotation. Extend rule configuration and durable work metadata with connection ID, configuration version, exact model ID, policy version, input category and billing mode. Store a non-secret execution snapshot before enqueueing. The queue carries identifiers only.

Scheduling, claim, the final check before each provider request, and persistence must validate tenant status, current rule consent, connection enabled state, config version and capability. Reject stale work rather than applying changed defaults. Keep results from disabled or reconfigured runs out of feedback. Reprocessing is explicit. Destination changes show affected rules and invalidate their consent/configuration until explicitly saved again. Credential rotation may use the latest validated credential for the same pinned destination, while every actual call records the credential version used. Prevent retiring versions still needed for provider-file cleanup. A request already dispatched cannot be unsent; disabling cancels it best-effort, rejects its later result and still performs cleanup. Dispatch timeouts can have an unknown billing outcome; do not promise exactly-once provider charges, and use provider idempotency only where supported.

Generalize processing-run provider/model metadata and single-provider database checks. Add normalized input/output/cache/reasoning token usage when reported, nullable when unavailable, and billing source. Do not invent dollar totals for unknown custom prices. Keep current reservation/refund semantics and record provider attempts separately so retries remain visible. Work cancelled before dispatch can release its reservation; a dispatched call can still be billed even if its result is rejected.

Generalize the Gemini file ledger to provider-file records with tenant, connection, endpoint/config version, credential version, remote file reference, processing run and deletion state. Existing file records map to the managed Gemini connection. Remote file IDs are unique within the provider connection/account, not globally. Inline media avoids remote persistence where supported. Disabling/deleting a connection stops new analysis immediately but retains only the encrypted cleanup credentials and references necessary to finish deletion, then purges them. Cleanup must also run after tenant suspension or deletion. Existing `ai_gemini_files` has cascading tenant/attachment foreign keys; redesign deletion ordering and retain a restricted cleanup tombstone before purging those parents, so remote deletion work is not lost. Tombstones hold no feedback content and expire under a documented cleanup retention policy. If the customer revokes external access first, show cleanup blocked, retain evidence and document provider expiry/manual cleanup; never falsely mark a remote file deleted.

Separate credential rotation from encryption-key rotation. Encrypted envelopes include a key ID and format version; share a small encryption primitive with existing integrations without forcing an unrelated migration of their secrets. Key-ring configuration must retain decrypt capability while cleanup references exist. For Bedrock, bearer API keys are distinct from AWS access-key pairs. The production path uses STS AssumeRole, a tenant-bound role ARN and external ID, and an explicit configured Fidibeki caller identity; no hidden fallback to a host's default AWS chain. Key-mode validation and inference use the documented bearer authorization mechanism. If key mode cannot discover models, use manual IDs and validated invocation. Reject a role trust policy that permits assumption without the correct external ID.

Provider identity, endpoint/region and consent records must reach disclosure, processing ledgers, export and erasure workflows. Public widget configuration includes only safe disclosure and supported inputs, never credentials or private connection details. Register customer-controlled destinations honestly in the provider inventory. Provider terms/data location are account-specific; do not claim every custom endpoint shares OpenAI's policies.

## Delivery sequence

1. Provider seam and regression protection. Extract shared analysis contract and guarded factory while managed Gemini remains the only active runtime. Acceptance: existing text/media, redaction, queue idempotency, lifecycle and cleanup tests still pass.
2. Connection storage and Google BYOK vertical slice. Add additive migrations, credential encryption, tenant permissions, connection APIs, discovery/test operations, settings UI and rule selection. Wire dynamic dispatch, pinned execution data and credential-aware file cleanup. Acceptance: two workspaces use separate Google keys/models; cross-tenant access fails; switching/disable/rotation races preserve routing and cleanup.
3. OpenAI, Anthropic and custom protocol adapters. Add text and verified screenshot support, model validation, protocol capability profiles, normalized errors/usage and guarded custom endpoints. Acceptance: contract fixtures validate requests/results, malformed output never persists, and SSRF/auth tests cover every outbound operation.
4. Amazon Bedrock. Implement Converse, region/model/profile handling, role/STS authentication and onboarding, plus explicit pilot key mode if useful. Acceptance: synthetic calls use the customer account, enforce external ID, honor region/profile permissions, refresh temporary credentials and fail safely on expiry/denial.
5. Complete product and rollout. Finish EN/PT copy, widget and SDK disclosures/types, privacy/export/erasure and provider release checks, usage/health views, staged enablement and operator runbook. Acceptance: E2E setup/model selection/invalid credentials/disable/retry flows and migration rehearsal pass across providers. Google BYOK in phase 2 already includes truthful public disclosure; later phases extend it before their flags can turn on.

All five phases are part of the requested scope. Implementation can ship incremental internal slices, but the work is not complete at Google BYOK. No source-code changes or deployment before user approval of this plan.

## Verification and release

- Go unit/contract tests use local provider fixtures; DB integration tests cover tenant RLS, permissions, version races, job replays, quota isolation, rotation and cleanup after account deletion.
- Preserve current Gemini behavior and legacy AI consent. Add schema migrations before enabling new writes; map old rules and historical runs to managed Gemini without changing whether AI is enabled. Drain or explicitly reconcile old in-flight jobs before enabling BYOK. Old workers must not receive new-format work.
- Add rollback controls that stop BYOK dispatch without silently rerouting it. Retain additive schema, processing evidence and cleanup workers until pending remote deletion finishes. Do not roll down over live connections.
- Frontend unit/type checks plus focused Playwright E2E for workspace settings, rule configuration, capability errors, EN/PT disclosures, accessible controls and masked credentials. Run relevant widget/mobile/Flutter checks if their public contracts change.
- Manual provider smoke tests use synthetic fixtures and explicitly supplied test accounts, never real customer feedback. Unknown provider capabilities stay disabled until tested.
- Release behind per-provider/workspace flags, retain managed Gemini for existing rules, pilot named workspaces, then broaden. Existing repository provider-governance checks remain part of release validation; this plan does not assert new providers are already approved.

## Review record

Gemini reviewed the complete supplied plan and Codex's repository findings through `agy`, using `gemini-3.1-pro-high`. It did not independently inspect the repository during the successful review. Conversation `8be8f803-dbdc-4c08-8543-c24a4399008e` returned full agreement with no blockers on the initial draft.

Codex accepted Gemini's suggestions for connection audit events and customer-configurable limits within server ceilings, and clarified Bedrock bearer keys versus role credentials. Codex declined the suggestion to allow unverified capability overrides: custom model IDs are welcome, but claimed screenshot support still needs validation. Codex also made the endpoint contract, BYOK-only startup, already-dispatched calls, file-ID scoping and deletion tombstones explicit. Gemini returned FINAL AGREEMENT with no remaining blockers on the revised plan and agreed with keeping capability validation mandatory. Model review is an architectural opinion, not runtime verification; implementation and provider smoke tests remain outstanding.

## Sources verified during planning

- Google recommends its native API when not already using OpenAI libraries and documents compatible model discovery. [Google compatibility documentation](https://ai.google.dev/gemini-api/docs/openai)
- OpenAI model listing returns availability/basic information. [OpenAI Models API](https://platform.openai.com/docs/api-reference/models/object?lang=curl)
- Anthropic provides model discovery and capability metadata. [List models](https://platform.claude.com/docs/en/api/models/list), [model metadata](https://platform.claude.com/docs/en/api/models)
- Anthropic defines structured output requests and schema constraints. [Structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs?m=1)
- Bedrock API compatibility varies by model and endpoint. [Supported APIs](https://docs.aws.amazon.com/bedrock/latest/userguide/apis.html), [model compatibility](https://docs.aws.amazon.com/bedrock/latest/userguide/models-api-compatibility.html)
- Bedrock supports bearer API keys; AWS recommends roles or temporary credentials for production. [API-key usage](https://docs.aws.amazon.com/en_us/bedrock/latest/userguide/api-keys-use.html), [production authentication guidance](https://docs.aws.amazon.com/en_en/bedrock/latest/userguide/getting-started-api-keys.html)
- AWS documents role assumption with external IDs for third-party access. [Third-party role access](https://docs.aws.amazon.com/en_en/IAM/latest/UserGuide/id_roles_common-scenarios_third-party.html)


## Implementation verification

Implemented in the isolated `feat/customer-ai-providers` worktree, including native adapters, connection management, model validation and selection, consent/disclosures, immutable queued destinations, credential/file cleanup, quotas, privacy exports and rollout documentation. See `docs/operations/customer-ai-providers.md`.

Verification includes the backend short suite, affected PostgreSQL integration suites, Redis concurrency isolation, AWS STS/signing and protocol fixtures, web/widget/React Native unit tests and type checks, and six desktop/mobile browser scenarios. The existing privacy export and AI policy tests were updated for connection metadata and generalized configuration-change evidence. Impeccable's independent UI finish review was completed and its model-default and focus fixes applied.

No live provider credentials were used, and no production flags were enabled. Flutter source and a contract test were updated; the Flutter SDK is unavailable in this environment, so Flutter validation remains for SDK CI.
