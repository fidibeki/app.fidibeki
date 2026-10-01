#!/usr/bin/env bash
# Real PostgreSQL/Redis/API/Nuxt/widget journey. All accounts and keys are fixtures.
set -euo pipefail
cd "$(dirname "$0")/.."
task_root="$PWD"
task_tmp="$(mktemp -d "${TMPDIR:-/tmp}/fidibeki-support.XXXXXX")"
task_pids=()
task_project="fidibeki-support-test-$$"
task_started=0
cleanup() {
  for task_pid in "${task_pids[@]:-}"; do kill "$task_pid" 2>/dev/null || true; done
  if [[ "$task_started" == 1 ]]; then docker compose -p "$task_project" -f infra/docker-compose.support-test.yml down >/dev/null 2>&1 || true; fi
  echo "Live support logs: $task_tmp"
}
trap cleanup EXIT
for tool in docker go node pnpm psql curl; do command -v "$tool" >/dev/null; done
task_started=1
docker compose -p "$task_project" -f infra/docker-compose.support-test.yml up -d --wait

if [[ "${SUPPORT_SKIP_BUILD:-0}" != 1 ]]; then
  pnpm --filter @fidibeki/widget build >"$task_tmp/widget-build.log" 2>&1
  pnpm --filter @fidibeki/web build >"$task_tmp/web-build.log" 2>&1
  pnpm --filter @fidibeki/backoffice build >"$task_tmp/backoffice-build.log" 2>&1
fi
(cd backend && go build -o "$task_tmp/api" ./cmd/fidibeki-api)
export APP_ENV=local APP_VERSION=live-support-test LOG_LEVEL=info
export HTTP_ADDRESS=127.0.0.1:8089 BASE_DOMAIN=localhost
export PUBLIC_BASE_URL=http://api.localhost:8089 WEB_PUBLIC_BASE_URL=http://api.localhost:4329 PLATFORM_BACKOFFICE_URL=http://admin.localhost:4179
export DATABASE_URL_APP='postgres://fidibeki_app:fidibeki-app-local@127.0.0.1:54339/fidibeki?sslmode=disable'
export DATABASE_URL_ADMIN='postgres://fidibeki_admin:fidibeki-admin-local@127.0.0.1:54339/fidibeki?sslmode=disable'
export DATABASE_URL_DIRECT='postgres://postgres:postgres@127.0.0.1:54339/fidibeki?sslmode=disable'
export REDIS_URL=redis://127.0.0.1:6389/0
export S3_ENDPOINT=http://127.0.0.1:9009 S3_PUBLIC_ENDPOINT=http://localhost:9009 S3_REGION=us-east-1 S3_BUCKET=fidibeki
# This journey submits text feedback; attachment/storage tests run separately.
export S3_ACCESS_KEY=fidibeki-local S3_SECRET_KEY=fidibeki-local-secret S3_AUTO_CREATE_BUCKET=false
export JWT_SECRET=local-only-jwt-secret-change-before-production INTERNAL_PROXY_SECRET=local-only-internal-proxy-secret-change-me
INTEGRATION_ENCRYPTION_KEY="$(node -p "require('node:crypto').randomBytes(32).toString('base64')")"
TWO_FACTOR_ENCRYPTION_KEY="$(node -p "require('node:crypto').randomBytes(32).toString('base64')")"
export INTEGRATION_ENCRYPTION_KEY TWO_FACTOR_ENCRYPTION_KEY
export EMAIL_TASK_SIGNING_KEY=ZWVlZWVlZWVlZWVlZWVlZWVlZWVlZWVlZWVlZWVlZWU=
export EMAIL_DRIVER=capture EMAIL_CAPTURE_DIR="$task_tmp/emails" MIGRATIONS_PATH="$task_root/infra/migrations"
"$task_tmp/api" migrate up >"$task_tmp/migrate.log" 2>&1
"$task_tmp/api" >"$task_tmp/api.log" 2>&1 & task_pids+=("$!")
SUPPORT_API_PORT=8089 SUPPORT_WIDGET_PORT=4180 node scripts/live-support-widget.mjs >"$task_tmp/widget.log" 2>&1 & task_pids+=("$!")
HOST=127.0.0.1 PORT=4329 NUXT_BASE_DOMAIN=localhost NUXT_PUBLIC_BASE_DOMAIN=localhost NUXT_PUBLIC_WIDGET_URL=http://widget.localhost:4180 NUXT_PUBLIC_WIDGET_KEY=pk_live_SUPPORTAAAAAAAAAAAAAAA NUXT_API_UPSTREAM=http://127.0.0.1:8089 NUXT_INTERNAL_PROXY_SECRET="$INTERNAL_PROXY_SECRET" node web/.output/server/index.mjs >"$task_tmp/web.log" 2>&1 & task_pids+=("$!")
HOST=127.0.0.1 PORT=4179 NUXT_BASE_DOMAIN=localhost NUXT_API_UPSTREAM=http://127.0.0.1:8089 NUXT_INTERNAL_PROXY_SECRET="$INTERNAL_PROXY_SECRET" BACKOFFICE_BASE_DOMAIN=localhost BACKOFFICE_API_UPSTREAM=http://127.0.0.1:8089 BACKOFFICE_INTERNAL_PROXY_SECRET="$INTERNAL_PROXY_SECRET" node backoffice/.output/server/index.mjs >"$task_tmp/backoffice.log" 2>&1 & task_pids+=("$!")
for task_url in http://127.0.0.1:8089/healthz http://127.0.0.1:4329/ http://127.0.0.1:4179/login http://127.0.0.1:4180/v1/loader.js; do
  task_ready=0
  for ((attempt=0; attempt<60; attempt++)); do
    if curl -s -o /dev/null "$task_url"; then task_ready=1; break; fi
    sleep 0.5
  done
  [[ "$task_ready" == 1 ]] || { echo "Server did not start: $task_url"; exit 1; }
done
SUPPORT_E2E_ISOLATED=1 SUPPORT_E2E_DATABASE_URL="$DATABASE_URL_DIRECT" SUPPORT_E2E_WEB_URL=http://support-e2e.localhost:4329 SUPPORT_E2E_ADMIN_URL=http://admin.localhost:4179 pnpm --filter @fidibeki/backoffice test:live-support
