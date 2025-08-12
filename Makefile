SHELL := /bin/bash

BINDIR ?= bin

GO := go

.PHONY: all build build-tools tidy test smoke fmt vet

all: tidy build build-tools

$(BINDIR):
	mkdir -p $(BINDIR)

build: $(BINDIR)
	$(GO) build -o $(BINDIR)/agentcli ./cmd/agentcli

build-tools: $(BINDIR)
	$(GO) build -o $(BINDIR)/timecli ./tools/timecli

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

# Basic smoke test requires a running OpenAI-compatible API at $OAI_BASE_URL or http://localhost:1234/v1
smoke: build build-tools
	OAI_BASE_URL=$${OAI_BASE_URL:-http://localhost:1234/v1} \
	OAI_MODEL=$${OAI_MODEL:-openai/gpt-oss-20b} \
	$(BINDIR)/agentcli -prompt "What's the local time in Helsinki? Use get_time." -tools ./tools.json -debug -model $$OAI_MODEL
