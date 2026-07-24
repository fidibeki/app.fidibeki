.PHONY: test test-short coverage format-check lint vulncheck governance-check migrate-up migrate-down widget-build widget-test widget-e2e dev down logs

test:
	cd backend && go test ./...

test-short:
	cd backend && go test -vet=off -short -p=8 ./...

coverage:
	cd backend && go test -p=1 ./... -count=1 -coverprofile=coverage.out
	cd backend && sh scripts/check-coverage.sh coverage.out

format-check:
	cd backend && test -z "$$(gofmt -l .)"

lint:
	cd backend && go vet ./...

# Local release gate for issue #63: fails if the provider governance registry is incomplete,
# diverges from the retention registry's evidenced provider set, falsely marks an entry approved,
# carries a stale review, or omits a personal-data category from the role matrix; and fails if the
# ROPA/DPIA processing-activity registry diverges from the retention/provider registries, drops or
# duplicates retention-category coverage, or claims a lawful-basis/DPIA decision without complete,
# current, non-placeholder evidence. This only detects drift BETWEEN these hand-maintained
# registries; it cannot discover a wholly new external adapter or processing purpose that was never
# added to any of them. Enables no provider and changes no runtime behavior.
governance-check:
	cd backend && go test ./internal/privacy/... -run 'Governance|ROPA|DPIA' -v

vulncheck:
	cd backend && go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

migrate-up:
	cd backend && go run ./cmd/fidibeki-api migrate up

migrate-down:
	cd backend && go run ./cmd/fidibeki-api migrate down

widget-build:
	pnpm --filter @fidibeki/widget build

widget-test:
	pnpm --filter @fidibeki/widget typecheck
	pnpm --filter @fidibeki/widget test

widget-e2e:
	pnpm --filter @fidibeki/widget test:e2e

dev:
	docker compose -f infra/docker-compose.yml up --build

down:
	docker compose -f infra/docker-compose.yml down

logs:
	docker compose -f infra/docker-compose.yml logs -f api
