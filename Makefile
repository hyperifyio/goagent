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

# Canonical list of tool binaries built under tools/bin in stable order
TOOLS := \
  get_time \
  exec \
  fs_read_file \
  fs_write_file \
  fs_append_file \
  fs_rm \
  fs_move \
  fs_search \
  fs_mkdirp \
  fs_apply_patch \
  fs_read_lines \
  fs_edit_range \
  fs_listdir \
  fs_stat

.PHONY: tidy build build-tools build-tool test clean lint fmtcheck verify-manifest-paths

tidy:
	$(GO) mod tidy

build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o bin/agentcli ./cmd/agentcli

build-tools:
	mkdir -p tools/bin
	@set -e; \
	for t in $(TOOLS); do \
	  echo "Building $$t"; \
	  GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/$$t$(EXE) ./tools/cmd/$$t; \
	done

# Build a single tool binary into tools/bin/$(NAME)
# Usage: make build-tool NAME=fs_read_file
build-tool:
	@set -eo pipefail; \
	if [ -z "$(NAME)" ]; then \
	  echo "Usage: make build-tool NAME=<name>"; \
	  echo "Available tools: $(TOOLS)"; \
	  exit 2; \
	fi; \
	case " $(TOOLS) " in \
	  *" $(NAME) "*) ;; \
	  *) echo "Unknown tool: $(NAME). Allowed: $(TOOLS)"; exit 2;; \
	esac; \
	mkdir -p tools/bin; \
	echo "Building $(NAME)"; \
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o tools/bin/$(NAME)$(EXE) ./tools/cmd/$(NAME)

test:
	$(GO) test ./...

clean:
	# Remove agent binary and each tool binary deterministically
	rm -f $(addprefix tools/bin/,$(addsuffix $(EXE),$(TOOLS)))
	rm -rf tools/bin
	rm -rf bin

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
	$(MAKE) check-tools-paths; \
	$(MAKE) verify-manifest-paths

# Verify tools.json commands use canonical ./tools/bin prefix for relative paths
# Fails if any command[0] is relative and does not start with ./tools/bin/
# Absolute paths are allowed for test fixtures. Requires ripgrep (rg).
verify-manifest-paths:
	@set -euo pipefail; \
	if ! command -v rg >/dev/null 2>&1; then \
		echo "ripgrep (rg) is required. Please install ripgrep."; \
		exit 1; \
	fi; \
	if [ ! -f tools.json ]; then \
		echo "tools.json not found at repo root"; \
		exit 1; \
	fi; \
	if rg -n -P --no-heading '"command"\s*:\s*\[\s*"(?!\./tools/bin/)(\./[^"]+)"' tools.json; then \
		echo "Invalid relative command[0] in tools.json. Use ./tools/bin/NAME or an absolute path."; \
		exit 1; \
	fi; \
	echo "verify-manifest-paths: OK"

fmtcheck:
	@echo "Checking gofmt..."; \
	files=$$(gofmt -s -l .); \
	if [ -n "$$files" ]; then \
		echo "Files need gofmt -s:"; echo "$$files"; exit 1; \
	fi

# Guard against legacy tool path usage outside canonical layout
# - Fails if any "./tools/(get_time|fs_*|exec)" invocation remains outside allowed paths
# - Also fails on single-file references like "./tools/<name>.go".
#   Allowed: "go build -o tools/bin/<name> ./tools/cmd/<name>". Forbidden: building directly from "./tools/<name>" outside `tools/cmd/**` and `tools/bin/**` (excluding FEATURE_CHECKLIST.md).
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
	# Single-file source builds or direct `go build/run` against ./tools/<name> are forbidden
	# Use PCRE2 to exclude allowed ./tools/cmd/* and ./tools/bin/* via negative lookahead
	if rg -n -P --no-heading --hidden \
		-g '!tools/cmd/**' -g '!tools/bin/**' -g '!FEATURE_CHECKLIST.md' -g '!.git/**' \
		-e '(\./tools/[a-z_]+\.go|go\s+(build|run)\s+.*\s\./tools/(?!cmd/|bin/)[a-z_]+)\b' .; then \
		echo "Direct tool source builds or single-file references found. Build from tools/cmd/NAME -> tools/bin/NAME."; \
		exit 1; \
	fi; \
	echo "check-tools-paths: OK"
