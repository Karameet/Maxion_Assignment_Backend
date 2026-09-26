# Requires `.env` at repo root (copy .env.example). On Windows run via Git Bash.
-include .env
export

.PHONY: db-up db-down db-nuke migrate migrate-down migrate-status migrate-test \
        run run-memory build test test-int docker-api-up docker-api-down docker-api-logs fmt vet

db-up:
	docker compose up -d db pgadmin

db-down:
	docker compose down

db-nuke:
	docker compose down -v

migrate:
	goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir migrations postgres "$(DATABASE_URL)" down

migrate-status:
	goose -dir migrations postgres "$(DATABASE_URL)" status

migrate-test:
	goose -dir migrations postgres "$(TEST_DATABASE_URL)" up

run:
	go run ./cmd/api

# No database needed: users, products and orders live in memory.
run-memory:
	ORDER_REPO=memory go run ./cmd/api

build:
	go build -o bin/api ./cmd/api

# Unit + handler tests. Needs no database (integration tests self-skip).
test:
	TEST_DATABASE_URL= go test ./...

# Integration tests TRUNCATE tables — they only ever use TEST_DATABASE_URL.
test-int: migrate-test
	go test ./... -count=1

docker-api-up:
	docker compose --profile api up -d --build api

docker-api-down:
	docker compose --profile api down

docker-api-logs:
	docker compose logs -f api

fmt:
	go fmt ./...

vet:
	go vet ./...
