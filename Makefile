BINARY     := bin/api
CMD        := ./cmd/api
COMPOSE    := docker compose -f docker/docker-compose.yml
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GOOSE      := $(shell go env GOPATH)/bin/goose
DB_URL     := $(shell grep -v '^\#' .env 2>/dev/null | grep DATABASE_URL | cut -d= -f2-)

.PHONY: env dev build test lint sqlc migrate-up migrate-down db-reset clean infra infra-down

## env: copy .env.example → .env (skips if .env already exists)
env:
	@test -f .env && echo ".env already exists, skipping" || (cp .env.example .env && echo "Created .env from .env.example")

## infra: start Postgres + MinIO in Docker
infra:
	$(COMPOSE) up -d

## infra-down: stop Docker services
infra-down:
	$(COMPOSE) down

## dev: start infra + wait for DB, then run the API (migrations run automatically on startup)
dev:
	$(COMPOSE) up -d
	@echo "Waiting for Postgres to be ready..."
	@until $(COMPOSE) exec -T postgres pg_isready -U community > /dev/null 2>&1; do sleep 1; done
	go run -ldflags="-X main.version=$(VERSION)" $(CMD)

## build: compile the binary
build:
	go build -ldflags="-X main.version=$(VERSION)" -o $(BINARY) $(CMD)

## test: run all tests
test:
	go test ./...

## lint: run go vet
lint:
	go vet ./...

## sqlc: regenerate database query code from SQL files
sqlc:
	sqlc generate

## migrate-up: manually run all pending migrations (useful for CI/debugging)
migrate-up:
	$(GOOSE) -dir migrations postgres "$(DB_URL)" up

## migrate-down: roll back the last migration
migrate-down:
	$(GOOSE) -dir migrations postgres "$(DB_URL)" down

## db-reset: wipe and re-apply all migrations from scratch
db-reset:
	$(GOOSE) -dir migrations postgres "$(DB_URL)" reset
	$(GOOSE) -dir migrations postgres "$(DB_URL)" up

## clean: remove build artifacts
clean:
	rm -rf bin/
