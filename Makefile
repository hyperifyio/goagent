SHELL := /bin/bash

GO ?= go
CGO_ENABLED ?= 0
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)

# Reproducible builds: trim local paths, strip symbols, disable VCS stamping,
# and clear build id for identical binaries across clean builds
BUILD_FLAGS ?= -trimpath -buildvcs=false
LD_FLAGS ?= -s -w -buildid=

# Pin golangci-lint to a version compatible with current Go
GOLANGCI_LINT_VERSION ?= v1.62.0

# Deterministic local bin directory for tool installs
GOBIN ?= $(CURDIR)/bin

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
  fs_stat \
  img_create \
  http_fetch \
  searxng_search \
  robots_check \
  readability_extract \
  metadata_extract \
  pdf_extract \
  rss_fetch \
  wayback_lookup \
  wiki_query \
  openalex_search \
  crossref_search

.PHONY: tidy build build-tools build-tool test clean clean-logs clean-all test-clean-logs lint lint-precheck fmt fmtcheck verify-manifest-paths bootstrap ensure-rg check-go-version install-golangci

tidy:
	$(GO) mod tidy

build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(BUILD_FLAGS) -ldflags '$(LD_FLAGS)' -o bin/agentcli ./cmd/agentcli

build-tools:
	mkdir -p tools/bin
	@set -e; \
	for t in $(TOOLS); do \
	  echo "Building $$t"; \
	  GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(BUILD_FLAGS) -ldflags '$(LD_FLAGS)' -o tools/bin/$$t$(EXE) ./tools/cmd/$$t; \
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
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(BUILD_FLAGS) -ldflags '$(LD_FLAGS)' -o tools/bin/$(NAME)$(EXE) ./tools/cmd/$(NAME)

test:
	$(GO) test ./...

clean:
	# Remove agent binary and each tool binary deterministically
	rm -f $(addprefix tools/bin/,$(addsuffix $(EXE),$(TOOLS)))
	rm -rf tools/bin
	rm -rf bin
	# Remove common test/build artifacts
	rm -f bin/coverage.out coverage.out
	rm -rf reports
	# Remove local audit/log artifacts created during tests
	rm -rf .goagent
	# Intentionally preserve logs/ here; see clean-logs for guarded deletion
	# rm -rf logs

# Guarded logs cleanup: only delete when STATE equals DOWN
# Usage:
#   make clean-logs                 # operates on ./logs (default)
#   make clean-logs LOGS_DIR=path   # operate on a specific logs dir (used by tests)
LOGS_DIR ?= logs
clean-logs:
	@set -euo pipefail; \
	DIR="$(LOGS_DIR)"; \
	if [ ! -d "$$DIR" ]; then \
	  echo "clean-logs: $$DIR not present; skipping"; \
	  exit 0; \
	fi; \
	STATE=$$(tr -d ' \t\r\n' < "$$DIR/STATE" 2>/dev/null || true); \
	if [ "$$STATE" = "DOWN" ]; then \
	  rm -rf "$$DIR"; \
	  echo "clean-logs: removed $$DIR"; \
	else \
	  echo "clean-logs: skipped ($$DIR/STATE='$$STATE')"; \
	fi

# Aggregate clean: normal clean then guarded logs cleanup
clean-all:
	@$(MAKE) clean
	@$(MAKE) clean-logs

# Fail early if the active Go toolchain (major.minor) differs from go.mod
# Usage: make check-go-version
check-go-version:
	@set -euo pipefail; \
    		# Extract version declared in go.mod and normalize to major.minor (ignore patch)
    		# Prefer `go mod edit -json` for robust parsing across environments
    		MOD_GO=$$(go mod edit -json | sed -nE 's/.*"Go"[[:space:]]*:[[:space:]]*"([0-9]+\.[0-9]+).*/\1/p' | head -n1 | tr -d ' \t\r\n'); \
        if [ -z "$$MOD_GO" ]; then echo "check-go-version: unable to parse major.minor from go.mod"; exit 2; fi; \
        # Extract system Go major.minor
		SYS_GO=$$(go env GOVERSION | sed -E 's/^go([0-9]+\.[0-9]+).*/\1/' | tr -d ' \t\r\n'); \
        if [ -z "$$SYS_GO" ]; then echo "check-go-version: unable to parse 'go version' output"; exit 2; fi; \
        if [ "$$SYS_GO" != "$$MOD_GO" ]; then \
          echo "Go toolchain mismatch: system $$SYS_GO != go.mod $$MOD_GO"; \
          echo "Hint: install Go $$MOD_GO (see https://go.dev/dl) or use a version manager, then re-run 'make check-go-version'."; \
          exit 2; \
        fi; \
        echo "check-go-version: OK (system $$SYS_GO matches go.mod $$MOD_GO)"

# Deterministic tests for clean-logs behavior across cases
# - DOWN => directory removed
# - non-DOWN => directory preserved
# - missing STATE => directory preserved
test-clean-logs:
	@set -euo pipefail; \
	TMP=$$(mktemp -d 2>/dev/null || mktemp -d -t clogs); \
	LD="$$TMP/logs"; \
	: # Case A: allowed removal when STATE=DOWN; \
	mkdir -p "$$LD"; \
	echo DOWN > "$$LD/STATE"; \
	touch "$$LD/file"; \
	$(MAKE) -s clean-logs LOGS_DIR="$$LD"; \
	if [ -d "$$LD" ]; then echo "test-clean-logs: expected removal when STATE=DOWN"; rm -rf "$$TMP"; exit 1; fi; \
	: # Case B: blocked when STATE!=DOWN; \
	mkdir -p "$$LD"; \
	echo UP > "$$LD/STATE"; \
	$(MAKE) -s clean-logs LOGS_DIR="$$LD"; \
	if [ ! -d "$$LD" ]; then echo "test-clean-logs: unexpected removal when STATE!=DOWN"; rm -rf "$$TMP"; exit 1; fi; \
	: # Case C: blocked when STATE missing; \
	rm -rf "$$LD"; \
	mkdir -p "$$LD"; \
	rm -f "$$LD/STATE"; \
	$(MAKE) -s clean-logs LOGS_DIR="$$LD"; \
	if [ ! -d "$$LD" ]; then echo "test-clean-logs: unexpected removal when STATE missing"; rm -rf "$$TMP"; exit 1; fi; \
	# Cleanup; \
	rm -rf "$$TMP"; \
	echo "test-clean-logs: OK"

lint:
	@$(MAKE) check-go-version
	@set -euo pipefail; \
		LINTBIN="$(GOBIN)/golangci-lint$(EXE)"; \
    # Fail fast if an existing linter is too old relative to MIN \
	$(MAKE) -s lint-precheck; \
	NEED_INSTALL=0; \
	if [ ! -x "$$LINTBIN" ]; then \
	  NEED_INSTALL=1; \
	else \
	  CUR_VER="$$($$LINTBIN version | sed -nE 's/.*version ([v0-9\.]+).*/\1/p')"; \
	  if [ "$$CUR_VER" != "$(GOLANGCI_LINT_VERSION)" ]; then NEED_INSTALL=1; fi; \
	fi; \
	if [ "$$NEED_INSTALL" = "1" ]; then \
	  echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION) into $(GOBIN)..."; \
	  $(MAKE) install-golangci; \
	fi; \
	"$$LINTBIN" version; \
	"$$LINTBIN" run --timeout=5m; \
	$(GO) vet ./...; \
	$(MAKE) fmtcheck; \
	$(MAKE) ensure-rg; \
	PATH="$(CURDIR)/bin:$$PATH" $(MAKE) check-tools-paths; \
	PATH="$(CURDIR)/bin:$$PATH" $(MAKE) verify-manifest-paths

# Verify ordering inside the lint target via make dry-run
# Ensures the first non-commented command is the sub-make invocation of check-go-version
# and that a golangci-lint version invocation appears later.
.PHONY: test-lint-order
test-lint-order:
	@set -euo pipefail; \
	OUT="$$(make -n lint 2>/dev/null)"; \
	FIRST_LINE="$$(printf '%s\n' "$$OUT" | sed -n '1p')"; \
	if [ "$$FIRST_LINE" != "make check-go-version" ]; then \
	  printf '%s\n' "test-lint-order: expected first line 'make check-go-version' but got: $$FIRST_LINE"; \
	  exit 1; \
	fi; \
	if ! printf '%s\n' "$$OUT" | awk '/golangci-lint/{found=1} END{exit found?0:1}'; then \
	  printf '%s\n' "test-lint-order: expected to find a golangci-lint invocation in dry-run output"; \
	  exit 1; \
	fi; \
	printf '%s\n' "test-lint-order: OK"

# Fail fast when golangci-lint is older than the minimum supported version
# Usage: make lint-precheck
lint-precheck:
	@set -euo pipefail; \
	LINTBIN="$(GOBIN)/golangci-lint$(EXE)"; \
	MIN="v1.60.0"; \
	if [ ! -x "$$LINTBIN" ]; then \
	  # Not installed yet; installation in the lint target will handle it \
	  echo "lint-precheck: $$LINTBIN not found; will install $(GOLANGCI_LINT_VERSION)"; \
	  exit 0; \
	fi; \
	GCL="$$($$LINTBIN version | sed -nE 's/.*version ([v0-9\.]+).*/\1/p')"; \
	if [ -z "$$GCL" ]; then \
	  echo "lint-precheck: unable to parse golangci-lint version from '$$($$LINTBIN version | head -n1)'"; \
	  exit 2; \
	fi; \
	MAX_VER="$$(printf '%s\n%s\n' "$$GCL" "$$MIN" | sort -V | tail -n1)"; \
	if [ "$$MAX_VER" != "$$GCL" ]; then \
	  echo "golangci-lint $$GCL < $$MIN with Go $$(go version) — update GOLANGCI_LINT_VERSION"; \
	  exit 2; \
	fi; \
	echo "lint-precheck: OK (golangci-lint $$GCL >= $$MIN)"

## Pin ripgrep for optional local, non-root install when missing
RG_VERSION ?= 14.1.0

# Ensure ripgrep (rg) is available; if not, download a static build into ./bin/rg
ensure-rg:
	@set -euo pipefail; \
	if command -v rg >/dev/null 2>&1; then \
	  exit 0; \
	fi; \
	echo "ripgrep (rg) not found; installing to ./bin/rg (version $(RG_VERSION))"; \
	mkdir -p bin; \
	OS=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
	ARCH=$$(uname -m); \
	case "$$OS" in \
	  linux) TOS=unknown-linux-musl;; \
	  darwin) TOS=apple-darwin;; \
	  *) echo "Unsupported OS for auto-install: $$OS"; exit 1;; \
	esac; \
	case "$$ARCH" in \
	  x86_64|amd64) TARCH=x86_64;; \
	  arm64|aarch64) TARCH=aarch64;; \
	  *) echo "Unsupported arch for auto-install: $$ARCH"; exit 1;; \
	esac; \
	URL="https://github.com/BurntSushi/ripgrep/releases/download/$(RG_VERSION)/ripgrep-$(RG_VERSION)-$$TARCH-$$TOS.tar.gz"; \
	TMP=$$(mktemp -d 2>/dev/null || mktemp -d -t rgdl); \
	echo "Downloading $$URL"; \
	if ! curl -fsSL "$$URL" | tar -xz -C "$$TMP"; then \
	  echo "Failed to download/extract ripgrep archive"; rm -rf "$$TMP"; exit 1; \
	fi; \
	SRC=$$(find "$$TMP" -type f -name rg -perm -u+x | head -n1); \
	if [ -z "$$SRC" ]; then \
	  echo "rg binary not found in archive"; rm -rf "$$TMP"; exit 1; \
	fi; \
	mv "$$SRC" bin/rg; \
	chmod +x bin/rg; \
	rm -rf "$$TMP"; \
	bin/rg --version | head -n1; \
	echo "ripgrep installed at ./bin/rg"

# Install pinned golangci-lint into ./bin using the local Go toolchain
install-golangci:
	@set -euo pipefail; \
	mkdir -p "$(GOBIN)"; \
	GOBIN="$(GOBIN)" GO111MODULE=on $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	"$(GOBIN)/golangci-lint$(EXE)" version

# Auto-format Go sources in-place using gofmt -s
fmt:
	@gofmt -s -w .

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

# Initialize and update git submodules (e.g., scripts and rules)
bootstrap:
	@git submodule update --init --recursive
