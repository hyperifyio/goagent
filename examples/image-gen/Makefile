# Simple helper to invoke ./tools/bin/img_create

# User-tunable vars (can be overridden):
PROMPT ?= tiny illustrative banner
SIZE ?= 1024x1024
N ?= 1
BASENAME ?= img
RETURN_B64 ?= 0
SAVE_DIR ?= assets

# Detect Windows executable suffix if present
IMG_CREATE := ./tools/bin/img_create
ifeq ($(OS),Windows_NT)
IMG_CREATE := ./tools/bin/img_create.exe
endif

.PHONY: help
help:
	@echo "Targets:"
	@echo "  run         - Invoke img_create with SAVE to $(SAVE_DIR)/$(BASENAME)_NNN.png"
	@echo "  run-b64     - Invoke img_create with return_b64=true"
	@echo "  clean       - Remove generated assets (rm -rf $(SAVE_DIR))"

.PHONY: run
run:
	@mkdir -p $(SAVE_DIR)
	@jq -n --arg p "$(PROMPT)" --arg s "$(SIZE)" --argjson n $(N) --arg dir "$(SAVE_DIR)" --arg base "$(BASENAME)" '{prompt:$p,n:$n,size:$s,save:{dir:$dir,basename:$base,ext:"png"}}' | $(IMG_CREATE)

.PHONY: run-b64
run-b64:
	@jq -n --arg p "$(PROMPT)" --arg s "$(SIZE)" --argjson n $(N) '{prompt:$p,n:$n,size:$s,return_b64:true}' | $(IMG_CREATE)

.PHONY: clean
clean:
	@rm -rf $(SAVE_DIR)
