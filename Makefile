.PHONY: build vet test run fmt tidy web-dev web-build web-lint \
        db-up db-down migrate-up migrate-down test-integration

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

run:
	go run ./cmd/rinfra-server

fmt:
	gofmt -w .

tidy:
	go mod tidy

web-dev:
	cd web && npm install && npm run dev

web-build:
	cd web && npm install && npm run build

web-lint:
	cd web && npm run lint && npx tsc --noEmit

# --- Database targets ---
# Requires Docker Compose. DATABASE_URL defaults to the compose instance.
DATABASE_URL ?= postgres://rinfra:rinfra@localhost:5432/rinfra?sslmode=disable

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

# Requires the golang-migrate CLI.
# Install: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1
# or download from https://github.com/golang-migrate/migrate/releases
migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down

test-integration: db-up
	@echo "Waiting for Postgres..."
	@until docker compose exec postgres pg_isready -U rinfra >/dev/null 2>&1; do sleep 1; done
	migrate -path migrations -database "$(DATABASE_URL)" up
	DATABASE_URL="$(DATABASE_URL)" go test -tags integration ./...
