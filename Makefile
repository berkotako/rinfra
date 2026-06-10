.PHONY: build vet test run fmt tidy web-dev web-build web-lint \
        db-up db-down migrate-up migrate-down test-integration dev

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

# dev — run the Go server (RINFRA_DEV=1, no Postgres needed) and the Next.js
# dev server together. Runs in two terminals because a single make rule with
# trap is fragile across shells; this documented two-terminal approach is simpler.
#
# Terminal 1:  make dev-server
# Terminal 2:  make dev-web
#
# Or, to run both in one terminal (Go server in background):
#   RINFRA_DEV=1 RINFRA_ADDR=:8080 go run ./cmd/rinfra-server &
#   SERVER_PID=$$! ; cd web && NEXT_PUBLIC_RINFRA_API=http://localhost:8080 npm run dev ; kill $$SERVER_PID
#
dev-server:
	RINFRA_DEV=1 RINFRA_ADDR=:8080 go run ./cmd/rinfra-server

dev-web:
	cd web && NEXT_PUBLIC_RINFRA_API=http://localhost:8080 npm run dev

# dev — convenience: start both in background (server) + foreground (web).
# Kills the server when the web process exits.
dev:
	@echo "Starting Go server in background (RINFRA_DEV=1)..."
	@RINFRA_DEV=1 RINFRA_ADDR=:8080 go run ./cmd/rinfra-server & \
	SERVER_PID=$$! ; \
	echo "Server PID: $$SERVER_PID — waiting 2s for startup..."; \
	sleep 2; \
	trap "kill $$SERVER_PID 2>/dev/null; exit" INT TERM EXIT; \
	cd web && NEXT_PUBLIC_RINFRA_API=http://localhost:8080 npm run dev; \
	kill $$SERVER_PID 2>/dev/null

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
