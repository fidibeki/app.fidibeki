# Fidibeki

Open-source infrastructure for collecting, triaging, and closing the loop on product feedback.

Fidibeki combines an embeddable feedback widget, multi-tenant workspace, issue workflows, privacy controls, optional session replay, and integrations—so feedback becomes work a team can act on.

> This project is early-stage. APIs and configuration may evolve as the public release matures.

## What’s included

- Go API with PostgreSQL row-level tenant isolation
- Embeddable JavaScript widget and React Native capture SDK
- Nuxt workspace, backoffice, and marketing applications
- Redis/Asynq background work and S3-compatible attachment storage
- Feedback, issue, notification, privacy, SAML, billing, and integration workflows

## Quick start

### Prerequisites

- Docker with Compose v2
- Node.js 24 and pnpm 11.15.1 for frontend work
- Go 1.25.12 for backend work outside Docker

Start the local stack:

```sh
docker compose -f infra/docker-compose.yml up -d --build
curl -i http://localhost:8088/healthz
```

| Service | Local address |
| --- | --- |
| API via Caddy | `http://localhost:8088` |
| Widget assets | `http://widget.localhost:8088` |
| MinIO console | `http://localhost:9001` |
| PostgreSQL | `localhost:5433` |
| Redis | `localhost:6380` |

Create the first platform administrator:

```sh
docker compose -f infra/docker-compose.yml run --rm api admin bootstrap --email admin@example.test
```

Use `api.localhost:8088` for platform routes and `<tenant>.localhost:8088` for tenant routes. Compose and `.env.example` contain development-only credentials; never deploy them.

## Add the widget

Create a project in the workspace, then add its public key to your site:

```html
<script src="http://widget.localhost:8088/v1/loader.js"
        data-fidibeki-key="pk_live_..." async></script>
```

The widget accepts requests only from configured allowed domains. See [mobile capture](docs/mobile-capture.md) for React Native integrations.

## Development

Run the API outside Docker after starting its dependencies:

```sh
docker compose -f infra/docker-compose.yml up -d postgres pgbouncer redis minio
cp .env.example backend/.env
make migrate-up
cd backend && go run ./cmd/fidibeki-api serve
```

Useful checks:

```sh
make test-short
make test
make coverage
make lint
pnpm --filter @fidibeki/web test
pnpm --filter @fidibeki/marketing test
pnpm --filter @fidibeki/widget test
pnpm --filter @fidibeki/mobile test
```

## Architecture

```text
Browser / mobile app → Widget or SDK → Go API → PostgreSQL (RLS)
                                           ├→ Redis / Asynq workers
                                           └→ S3-compatible object storage
```

Frontend applications live in `web/`, `backoffice/`, and `marketing/`; the widget is in `widget/`, and mobile packages are in `mobile/` and `examples/mobile/`.

## Documentation

- [Mobile capture](docs/mobile-capture.md)
- [Transactional email](docs/transactional-email.md)
- [SAML single sign-on](docs/saml-sso.md)
- [Privacy and retention](docs/privacy-data-map.md)

## Contributing and security

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before opening an issue or pull request.

For vulnerabilities, follow [SECURITY.md](SECURITY.md); do not disclose security issues in public tickets.

## License

Fidibeki is licensed under the [GNU Affero General Public License v3.0](LICENSE). If you modify and run it for users over a network, you must offer them the corresponding source code.
