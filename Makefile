.PHONY: build vet test run fmt tidy web-dev web-build web-lint

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
