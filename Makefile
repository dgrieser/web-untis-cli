VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/dgrieser/web-untis-cli/internal/cli.Version=$(VERSION)

.PHONY: build install test vet fmt

build:
	go build -ldflags '$(LDFLAGS)' -o bin/webuntis ./cmd/webuntis

install:
	go install -ldflags '$(LDFLAGS)' ./cmd/webuntis

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .
