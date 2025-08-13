SHELL := /bin/bash

GO ?= go
CGO_ENABLED ?= 0
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)

.PHONY: tidy build build-tools test clean lint fmtcheck

tidy:
	$(GO) mod tidy

build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o bin/agentcli ./cmd/agentcli

build-tools:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/get_time ./tools/get_time.go
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/exec ./tools/exec

test:
	$(GO) test ./...

clean:
	rm -rf bin tools/get_time tools/exec

lint:
	@set -euo pipefail; \
	if ! command -v golangci-lint >/dev/null 2>&1; then \
		 echo "Installing golangci-lint..."; \
		 GO111MODULE=on $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.59.1; \
	fi; \
	golangci-lint version; \
	golangci-lint run --timeout=5m; \
	$(GO) vet ./...; \
	$(MAKE) fmtcheck

fmtcheck:
	@echo "Checking gofmt..."; \
	files=$$(gofmt -s -l .); \
	if [ -n "$$files" ]; then \
		echo "Files need gofmt -s:"; echo "$$files"; exit 1; \
	fi
