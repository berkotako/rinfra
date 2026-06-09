.PHONY: build vet test run fmt tidy

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
