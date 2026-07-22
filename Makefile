.PHONY: test test-short coverage format-check lint vulncheck migrate-up migrate-down widget-build widget-test widget-e2e dev down logs

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
