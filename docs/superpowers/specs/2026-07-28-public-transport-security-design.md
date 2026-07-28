# Public Transport Security Design

**Issue:** [#154](https://github.com/fidibeki/app.fidibeki/issues/154)  
**Policy:** `Strict-Transport-Security: max-age=31536000`  
**Scope:** Public API, application, widget, backoffice, and marketing responses

## Context

Railway's edge terminates TLS and forwards requests to each service, but the
current HTTPS responses do not establish an HSTS policy. HTTP-to-HTTPS
redirects therefore leave the first HTTP request downgradeable.

The repository has five browser-reachable response surfaces:

- the Go API;
- the Nuxt workspace application;
- the Caddy widget edge;
- the Nuxt platform backoffice; and
- the static Caddy marketing site.

The staging environment currently runs the API, workspace application, and
widget. Backoffice and marketing coverage is implemented and tested locally/CI
so their next authorized deployment has the same policy.

Railway documents that its edge terminates TLS and returns the service response
through that edge:
<https://docs.railway.com/networking/edge-networking>.

## Decision

Each public service emits:

```text
Strict-Transport-Security: max-age=31536000
```

The header is set by application or container configuration, after Railway's
TLS termination:

- a global Go middleware covers every API response, including health, error,
  redirect, and host-dispatch paths;
- global Nitro server middleware covers every web and backoffice response,
  including proxied API responses;
- global Caddy headers cover widget and marketing responses.

The policy is emitted consistently in every runtime. User agents ignore HSTS
received over insecure transport, so local HTTP behavior is unchanged while
local and image-level tests can still inspect the exact response header.

## Deliberate Omissions

`includeSubDomains` is not enabled. The production apex, workspace wildcard,
API, widget, backoffice, status, and any future host must first have complete
certificate and HTTPS rollback coverage. Applying it prematurely at the apex
could make an unrelated or not-yet-migrated subdomain unavailable.

`preload` is not enabled. Preload is a separate, difficult-to-reverse production
decision and requires explicit approval plus conformance with the browser preload
program.

The one-year max age matches the issue recommendation. Reducing it would weaken
the intended protection; increasing scope with either omitted directive would
create a separate operational commitment.

## Regression Coverage

- Go middleware tests cover the exact header on normal and error responses.
- Server tests prove the middleware is installed globally.
- Web and backoffice Playwright checks inspect health, page, and proxied
  responses.
- Widget CI validates the header from the built Caddy image.
- A marketing image workflow builds the production image and validates its
  health and document responses.
- The staging smoke checks HTTPS HSTS and HTTP redirects for the configured app,
  tenant, API, and widget URLs. It polls during a rollout so the first completed
  service does not create a false failure while the other services are still
  deploying.

The smoke uses explicit environment URLs rather than deriving sibling hosts.
This keeps the check correct if a service moves to a different hostname.

## Rollout And Closure

1. Merge only after all affected local and CI gates pass.
2. Configure the explicit staging app and widget URLs in the GitHub staging
   environment.
3. Deploy only the affected staging API, web, and widget services.
4. Verify the exact HTTPS header and equivalent HTTP redirect on every configured
   staging host.
5. Record staging evidence on #154.

Production is not deployed under the current staging-only direction. The code
is production-ready, but #154 remains open until an explicitly authorized
production promotion proves the same header and redirect behavior on production
application, API, widget, backoffice, and marketing hosts.
