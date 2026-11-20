.PHONY: run prometheus build test race fmt vet

run:
	go run ./cmd/cache-server

prometheus:
	prometheus --config.file=prometheus.yml --web.listen-address=127.0.0.1:9090 --storage.tsdb.path=prometheus-data

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
