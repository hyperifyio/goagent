SHELL := /bin/bash

GO ?= go
CGO_ENABLED ?= 0
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)

# Executable suffix for Windows builds
EXE :=
ifeq ($(GOOS),windows)
EXE := .exe
endif

.PHONY: tidy build build-tools test clean lint fmtcheck

tidy:
	$(GO) mod tidy

build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o bin/agentcli ./cmd/agentcli

build-tools:
	mkdir -p tools/bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/get_time$(EXE) ./tools/timecli
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/exec$(EXE) ./tools/exec
    GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_read_file$(EXE) ./tools/cmd/fs_read_file
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_write_file$(EXE) ./tools/fs_write_file
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_append_file$(EXE) ./tools/fs_append_file
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_rm$(EXE) ./tools/fs_rm
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_move$(EXE) ./tools/fs_move
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_search$(EXE) ./tools/fs_search
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_mkdirp$(EXE) ./tools/cmd/fs_mkdirp
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_apply_patch$(EXE) ./tools/fs_apply_patch
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_read_lines$(EXE) ./tools/fs_read_lines
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_edit_range$(EXE) ./tools/fs_edit_range
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_listdir$(EXE) ./tools/fs_listdir
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/fs_stat$(EXE) ./tools/fs_stat

test:
	$(GO) test ./...

clean:
	# Remove standard binary directories
	rm -rf bin tools/bin
	# Remove legacy single-file tool binaries at tools/<NAME> (files only)
	rm -f tools/get_time$(EXE) tools/exec$(EXE) tools/fs_read_file$(EXE) tools/fs_write_file$(EXE) tools/fs_append_file$(EXE) tools/fs_rm$(EXE) tools/fs_move$(EXE) tools/fs_search$(EXE) tools/fs_mkdirp$(EXE) tools/fs_apply_patch$(EXE) tools/fs_read_lines$(EXE) tools/fs_edit_range$(EXE) tools/fs_listdir$(EXE) tools/fs_stat$(EXE) || true
	# Remove legacy subdir-built binaries at tools/*/<NAME>
	rm -f tools/timecli/get_time$(EXE) \
	      tools/exec/exec$(EXE) \
	      tools/fs_read_file/fs_read_file$(EXE) \
	      tools/fs_write_file/fs_write_file$(EXE) \
	      tools/fs_append_file/fs_append_file$(EXE) \
	      tools/fs_rm/fs_rm$(EXE) \
	      tools/fs_move/fs_move$(EXE) \
	      tools/fs_search/fs_search$(EXE) \
	      tools/fs_mkdirp/fs_mkdirp$(EXE) \
	      tools/fs_apply_patch/fs_apply_patch$(EXE) \
	      tools/fs_read_lines/fs_read_lines$(EXE) \
	      tools/fs_edit_range/fs_edit_range$(EXE) \
	      tools/fs_listdir/fs_listdir$(EXE) \
	      tools/fs_stat/fs_stat$(EXE) || true

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
