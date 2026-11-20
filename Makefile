# Canopy development tasks. Run `make infra` first.
SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker-compose.dev.yml
SQLC := docker run --rm -v "$(CURDIR)/hub":/src -w /src --user "$(shell id -u):$(shell id -g)" sqlc/sqlc:latest

HUB_ENV := DATABASE_URL=postgres://canopy:canopy@localhost:5432/canopy?sslmode=disable \
	REDIS_URL=redis://localhost:6379 \
	PUBLIC_URL=http://localhost:8000 \
	COOKIE_SECURE=false TRUST_PROXY=true \
	SMTP_ADDR=localhost:1025

.PHONY: infra infra-down hub web gen test system lint build release

infra: ## Postgres, Redis, RustFS, Mailpit, Caddy (http://localhost:8000)
	$(COMPOSE) up -d

infra-down:
	$(COMPOSE) down

hub: ## API on :8080 against the dev Postgres; prints a setup link on first start
	cd hub && $(HUB_ENV) go run ./cmd/hub serve --data ../.dev-data

web: ## Next.js on :3000 (open http://localhost:8000)
	cd frontend && pnpm dev

gen: ## sqlc, TypeScript view types (tygo) and API types (OpenAPI)
	$(SQLC) generate
	cd hub && go run github.com/gzuidhof/tygo@latest generate
	cd frontend && pnpm exec openapi-typescript ../shared/openapi.yaml -o src/lib/api-types.ts

test: ## Go unit and integration tests (needs `make infra`)
	go test -race ./hub/... ./client/... ./shared/...

system: ## hub + client + fakeclaude as real processes (needs `make infra`)
	cd e2e/system && go test -count=1 ./...

lint:
	go vet ./hub/... ./client/... ./shared/...
	cd frontend && pnpm lint && pnpm exec tsc --noEmit

build: ## development binaries (no embedded web app)
	cd hub && go build -o bin/canopy-hub ./cmd/hub
	cd client && go build -o bin/canopy ./cmd/canopy && go build -o bin/fakeclaude ./cmd/fakeclaude

release: ## dist/canopy-hub-<os>-<arch> and dist/canopy-<os>-<arch> (PLATFORMS="linux/amd64" for one)
	scripts/release.sh
