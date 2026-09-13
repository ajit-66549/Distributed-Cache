.PHONY: run build test race fmt vet

run:
	go run ./cmd/cache-server

build:
	go build -o bin/cache-server ./cmd/cache-server

test:
	go test ./...

race:
	go test -race ./...

fmt:
	go fmt ./...

vet:
	go vet ./...