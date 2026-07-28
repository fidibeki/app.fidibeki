# Fidibeki backoffice

Standalone Nuxt console for platform administrators. It is intentionally a
separate browser origin from the tenant application.

## Run locally

```sh
pnpm --filter @fidibeki/backoffice dev
```

The local server listens on port 3000 by default. Use a different port from the
tenant web app, for example `--port 3001`, and set:

```sh
BACKOFFICE_API_UPSTREAM=http://127.0.0.1:8088
BACKOFFICE_INTERNAL_PROXY_SECRET=local-only-internal-proxy-secret-change-me
BACKOFFICE_BASE_DOMAIN=localhost
BACKOFFICE_TRUSTED_PROXY_CIDRS=127.0.0.0/8,::1/128
BACKOFFICE_TRUSTED_PROXY_CIDRS=127.0.0.0/8,::1/128
```

The Go API must receive the matching `PLATFORM_BACKOFFICE_URL`, e.g.
`http://admin.localhost:3001`. The Nuxt proxy overwrites the trusted internal
host/secret headers and only exposes explicitly allow-listed `/admin/*` paths.
It also forwards the browser address for login throttling; the API consumes that
address only after the matching internal proxy secret has been verified. The
forwarding chain is accepted only from `BACKOFFICE_TRUSTED_PROXY_CIDRS`; leave
that setting empty only when Nuxt receives direct browser connections.

## Production configuration

Deploy the backoffice at a dedicated `admin` origin. Set:

```sh
PLATFORM_BACKOFFICE_URL=https://admin.fidibeki.com
BACKOFFICE_API_UPSTREAM=http://api:8080
BACKOFFICE_INTERNAL_PROXY_SECRET=<same value as INTERNAL_PROXY_SECRET>
BACKOFFICE_BASE_DOMAIN=fidibeki.com
BACKOFFICE_TRUSTED_PROXY_CIDRS=<CIDRs of the ingress proxies immediately before Nuxt>
```

Do not expose `BACKOFFICE_INTERNAL_PROXY_SECRET` in browser-visible runtime
config, and never point the proxy at a tenant host.
Set `BACKOFFICE_TRUSTED_PROXY_CIDRS` to the private ingress networks that can
reach Nuxt in production. If it is empty, forwarding headers are ignored and
the direct socket peer is throttled instead.

## Bootstrap the root administrator

The root account is passwordless; it can only receive a short-lived magic link.
Run this once per environment after migrations succeed:

```sh
docker compose -f infra/docker-compose.yml run --rm api \
  admin bootstrap --email acidiney.dias@fidibeki.com
```

The command is idempotent and grants the complete platform permission set. It
must be run against the intended environment database; do not copy refresh
tokens, magic-link tokens, or passwords between environments.

## Verification

```sh
pnpm --filter @fidibeki/backoffice test
pnpm --filter @fidibeki/backoffice typecheck
pnpm --filter @fidibeki/backoffice build
make test-short
```
