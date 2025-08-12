SHELL := /bin/bash

GO ?= go
CGO_ENABLED ?= 0

.PHONY: tidy build build-tools test clean lint

 tidy:
	$(GO) mod tidy

 build:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o bin/agentcli ./cmd/agentcli

 build-tools:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/get_time ./tools/get_time.go

 test:
	$(GO) test ./...

 clean:
	rm -rf bin tools/get_time

 lint:
	@echo "Lint placeholder: configure golangci-lint in a later step"
