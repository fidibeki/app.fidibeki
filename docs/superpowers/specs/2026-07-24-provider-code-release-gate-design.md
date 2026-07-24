# Provider Code Release Gate Design

## Status And Scope

This is a local-only follow-up slice for issue #63. It closes one limitation recorded by the
provider-governance foundation: validating the governance and retention registries against each
other does not detect a new external adapter when both registries are left unchanged.

The slice adds no provider, legal approval, contract, credential, environment change, deployment,
or production work. It does not claim that a registered provider is active in an environment.

## Audit

The repository currently has three ways to introduce an external processing boundary:

1. a direct Go dependency that owns a network client, such as Stripe, Gemini, Sentry, Redis,
   PostgreSQL, or MinIO;
2. a direct browser dependency with its own delivery path, currently `@sentry/browser`;
3. a standard-library HTTP request, currently Resend, Turnstile, and customer-configured
   Slack/Teams/generic webhooks.

`make governance-check` validates legal/governance field completeness and exact parity with
`RetentionProvider` resources. It does not inspect those code entry points. The Sentry governance
row, for example, cites only the backend SDK even though the widget also depends directly on
`@sentry/browser`.

## Decision

Add a repository-aware provider code gate in the privacy package tests:

- classify every direct dependency from `backend/go.mod` and every dependency from the three
  workspace `package.json` files as either a governed provider boundary, an application/library
  dependency, or a test/build dependency;
- fail on an unclassified or stale dependency, so adding a provider SDK requires an explicit
  governance decision in the same change;
- parse non-test Go source with `go/parser` and inventory standard-library outbound HTTP request
  constructors;
- allow those constructors only at explicit provider-owned call sites;
- attach at least one verified file/signal or dependency to every provider-governance row and
  reject missing, stale, unknown, or duplicate evidence;
- include the widget Sentry dependency in the existing Sentry row's evidence;
- run the gate from `make governance-check` and therefore from the ordinary privacy test suite.

The dependency and source inventories are intentionally exact. Adding an ordinary library also
requires classifying it, but does not require calling it a provider. This is a small review cost in
exchange for preventing a network-capable SDK from being introduced without any governance
decision.

## Boundaries

Static checks cannot prove that arbitrary code never opens a socket, invokes a binary, or receives
an endpoint dynamically. The gate covers the supported adapter patterns in this repository:
declared direct dependencies and standard-library HTTP construction. Review remains responsible
for rejecting evasive or novel transport patterns.

The gate proves code presence and review coverage only. Provider entity, location, transfer,
contract, retention, owner, and approval fields remain pending until founder/legal evidence exists.

## Verification

- Unit tests must prove an unclassified/stale dependency fails.
- Unit tests must prove an unapproved HTTP call site fails.
- Unit tests must prove a governance provider without current code evidence fails.
- The live repository inventory must pass.
- `make governance-check`, `go test ./...`, `go vet ./...`, `go build ./...`, formatting, and
  `git diff --check` must pass before integration.
