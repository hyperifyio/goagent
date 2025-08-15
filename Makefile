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
	mkdir -p tools/bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/get_time ./tools/timecli
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/exec ./tools/exec
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_read_file ./tools/fs_read_file.go
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_write_file ./tools/fs_write_file
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_append_file ./tools/fs_append_file
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_rm ./tools/fs_rm
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_move ./tools/fs_move
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_search ./tools/fs_search
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_mkdirp ./tools/fs_mkdirp
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_apply_patch ./tools/fs_apply_patch
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_read_lines ./tools/fs_read_lines
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_edit_range ./tools/fs_edit_range
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_listdir ./tools/fs_listdir
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_stat ./tools/fs_stat

test:
	$(GO) test ./...

clean:
	rm -rf bin tools/bin tools/get_time tools/exec tools/fs_read_file tools/fs_write_file tools/fs_append_file tools/fs_rm tools/fs_move tools/fs_search tools/fs_mkdirp tools/fs_apply_patch tools/fs_read_lines tools/fs_edit_range tools/fs_listdir tools/fs_stat

lint:
	@set -euo pipefail; \
	if ! command -v golangci-lint >/dev/null 2>&1; then \
		 echo "Installing golangci-lint..."; \
		 GO111MODULE=on $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.59.1; \
	fi; \
	golangci-lint version; \
	golangci-lint run --timeout=5m; \
	$(GO) vet ./...; \
	$(MAKE) fmtcheck; \
	$(MAKE) check-tools-paths

fmtcheck:
	@echo "Checking gofmt..."; \
	files=$$(gofmt -s -l .); \
	if [ -n "$$files" ]; then \
		echo "Files need gofmt -s:"; echo "$$files"; exit 1; \
	fi

# Guard against legacy tool path usage outside canonical layout
# - Fails if any "./tools/(get_time|fs_*|exec)" invocation remains outside allowed paths
# - Also fails on single-file references like "./tools/name.go" or direct builds/runs like
#   "go build ... ./tools/name" outside `tools/cmd/**` and `tools/bin/**` (excluding FEATURE_CHECKLIST.md).
# Requires ripgrep (`rg`).
check-tools-paths:
	@set -euo pipefail; \
	if ! command -v rg >/dev/null 2>&1; then \
		echo "ripgrep (rg) is required. Please install ripgrep."; \
		exit 1; \
	fi; \
	# Legacy invocations of tools outside canonical layout
	if rg -n --no-heading --hidden \
		-g '!tools/cmd/**' -g '!tools/bin/**' -g '!FEATURE_CHECKLIST.md' -g '!.git/**' \
		-e '\./tools/(get_time|fs_[a-z_]+|exec)\b' .; then \
		echo "Forbidden legacy tool path references found. Use ./tools/bin/NAME or sources under tools/cmd/NAME."; \
		exit 1; \
	fi; \
	# Single-file or direct tool builds outside canonical layout
	if rg -n --no-heading --hidden \
		-g '!tools/cmd/**' -g '!tools/bin/**' -g '!FEATURE_CHECKLIST.md' -g '!.git/**' \
		-e '(\./tools/[a-z_]+\.go|go\s+(build|run)\s+.*\./tools/[a-z_]+)\b' .; then \
		echo "Direct tool source builds or single-file references found. Build from tools/cmd/NAME -> tools/bin/NAME."; \
		exit 1; \
	fi; \
	echo "check-tools-paths: OK"
