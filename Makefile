BINARY     := bin/api
CMD        := ./cmd/api
COMPOSE    := docker compose -f docker/docker-compose.yml
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: env dev build test lint migrate-up migrate-down db-reset clean

## env: copy .env.example → .env (skips if .env already exists)
env:
	@test -f .env && echo ".env already exists, skipping" || (cp .env.example .env && echo "Created .env from .env.example")

## dev: start docker services, then run the API
dev:
	$(COMPOSE) up -d
	@echo "Waiting for services..."
	@sleep 2
	go run -ldflags="-X main.version=$(VERSION)" $(CMD)

## build: compile the binary
build:
	go build -ldflags="-X main.version=$(VERSION)" -o $(BINARY) $(CMD)

## test: run all tests
test:
	go test ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## migrate-up: run all pending migrations (placeholder)
migrate-up:
	@echo "No migrations yet"

## migrate-down: roll back last migration (placeholder)
migrate-down:
	@echo "No migrations yet"

## db-reset: drop and re-apply all migrations (placeholder)
db-reset:
	@echo "No migrations yet"

## clean: remove build artifacts
clean:
	rm -rf bin/
