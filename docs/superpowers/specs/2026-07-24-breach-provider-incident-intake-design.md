# Breach Provider Incident Intake Design

## Status And Scope

This is the next local-only vertical slice of issue #64. It records an external provider incident
notice against the restricted personal-data breach register and maps the notice to affected tenants
and governed data-flow categories.

It performs no deployment, provider communication, regulatory/customer notification, production
work, or legal classification. The existing provider-governance registry currently leaves legal
entity, contract, transfer, and approval fields pending; this slice must preserve that uncertainty.

## Audit

The breach register already supports `subprocessor_notice` as an incident detection source and
`third_party_subprocessor` as an affected environment. Those enum values do not create a provider
notice record. The register currently cannot answer:

- which governed external boundary sent or owns the notice;
- when the provider says the event occurred or when Fidibeki received the notice;
- which affected tenants and governed processing/data-flow categories were mapped from it;
- which frozen provider-governance facts were known at intake time;
- which bounded evidence references support the mapping.

The provider-governance registry exposes 12 stable provider IDs, a registry version, boundary kind,
display name, primary retention-review category, and explicit pending/unknown legal fields. Not
every row is a subprocessor: rows may be infrastructure providers, service providers,
customer-configured destinations, or an internal runtime boundary.

## Decision

Add an append-only `breach_provider_incident_intakes` ledger outside the `fidibeki_app` grant
boundary. Each intake records:

- one existing breach incident and a server-issued stable intake ID/sequence;
- one provider ID that exists in the current governance registry;
- a frozen snapshot of governance registry version, display name, boundary kind, approval status,
  contract status, and primary retention-review category;
- provider notice reference, provider-reported time, server-authoritative received/created time,
  bounded factual summary, and bounded evidence references;
- one or more governed processing/data-flow category IDs from the current role matrix;
- the platform-admin actor plus idempotency key/fingerprint.

An internal-runtime boundary is not a valid external provider incident source. Pending governance
fields remain pending in the frozen snapshot. The route and UI must use “provider incident intake”
or “external provider notice”; it must not label the row a confirmed subprocessor notice unless a
future legally approved registry supports that statement.

Add `breach_provider_incident_tenants` as an append-only mapping from intake to opaque tenant IDs.
Every mapped tenant must already be an affected tenant on the parent incident. This prevents an
intake response from expanding tenant scope independently or exposing tenant names, slugs, billing
data, contacts, or content.

`provider_reported_at` is an external fact and may precede Fidibeki awareness.
`received_at`/`created_at` come from one server clock read and may not precede the incident's
immutable `awareness_at`. The intake never rewrites the Article 33 clock; a mismatch remains visible
for explicit assessment rather than silently changing history.

## API

Add one platform-admin route:

`POST /admin/breach-incidents/{incidentID}/provider-intakes`

It reuses `platform:breach:append`, requires `Idempotency-Key`, and accepts provider ID, provider
notice reference, provider-reported time, factual summary, evidence references, affected tenant
IDs, and processing/data-flow category IDs. Unknown providers/categories, internal runtime
boundaries, unrelated tenants, future timestamps, blank/oversized values, and idempotency
fingerprint conflicts fail closed.

Incident detail additively returns `provider_incident_intakes`. Document exports must include the
same frozen facts, tenant mappings, data-flow mappings, timestamps, actor, and evidence references.

## Privacy And Failure Boundaries

- No raw provider payload, attachment, reporter content, credential, token, signed URL, IP address,
  user agent, tenant display data, or provider error body is stored.
- Evidence references are bounded opaque references, never embedded evidence.
- Tables remain `fidibeki_admin` only and reject update/delete.
- Provider and category validation happens against the same code-owned governance snapshot that is
  persisted, so a retry cannot resolve against a different registry version silently.
- Intake creation, tenant mappings, and idempotency evidence commit atomically.
- No external request or outbox event is created.

## Verification

- Domain tests cover registry/provider/category validation, internal-runtime rejection, bounds,
  timestamp rules, normalization, and deterministic fingerprints.
- PostgreSQL tests cover idempotency/conflict, incident/tenant scope, append-only triggers, grants,
  stable snapshot persistence, and migration up/down/up.
- HTTP tests cover platform-admin audience/permission, malformed IDs/input, not found, conflict,
  success, and additive detail visibility.
- Export tests cover JSON/Markdown parity and structural safety for the new intake facts.
- Full backend test, vet, build, format, governance, retention-registry, and migration gates pass.
