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
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/get_time ./tools/timecli
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/exec ./tools/exec
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_read_file ./tools/fs_read_file.go
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_write_file ./tools/fs_write_file
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_append_file ./tools/fs_append_file
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_rm ./tools/fs_rm
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_move ./tools/fs_move
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_search ./tools/fs_search
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_mkdirp ./tools/fs_mkdirp
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_apply_patch ./tools/fs_apply_patch
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_read_lines ./tools/fs_read_lines
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_edit_range ./tools/fs_edit_range
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_listdir ./tools/fs_listdir
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/fs_stat ./tools/fs_stat

test:
	$(GO) test ./...

clean:
	rm -rf bin tools/get_time tools/exec tools/fs_read_file tools/fs_write_file tools/fs_append_file tools/fs_rm tools/fs_move tools/fs_search tools/fs_mkdirp tools/fs_apply_patch tools/fs_read_lines tools/fs_edit_range tools/fs_listdir tools/fs_stat

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
