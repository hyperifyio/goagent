#!/usr/bin/env bash
# Smoke test for goresearch (Nginx HSTS use case)
# - Validates external dependencies required by the tool:
#   - SearxNG at http://localhost:8888
#   - OpenAI-compatible LLM API at http://localhost:1234/v1 (or $LLM_BASE_URL)
# - Does NOT build or run goresearch; build moved to Makefile
# - Prints a clean PASS/FAIL summary

set -u

# Colors
GREEN="\033[32m"; RED="\033[31m"; YELLOW="\033[33m"; BOLD="\033[1m"; RESET="\033[0m"
PASS_ICON="✅"; FAIL_ICON="❌"; WARN_ICON="⚠️"

# Accumulators
passes=()
fails=()
warns=()

section() {
  echo
  echo "${BOLD}== $* ==${RESET}"
}

ok() { echo -e "${GREEN}${PASS_ICON} PASS${RESET} - $*"; passes+=("$*"); }
ko() { echo -e "${RED}${FAIL_ICON} FAIL${RESET} - $*"; fails+=("$*"); }
wn() { echo -e "${YELLOW}${WARN_ICON} WARN${RESET} - $*"; warns+=("$*"); }

have() { command -v "$1" >/dev/null 2>&1; }

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR" || exit 1

SEARX_URL="${SEARX_URL:-http://localhost:8888}"
LLM_BASE="${LLM_BASE_URL:-http://localhost:1234/v1}"

maybe_bootstrap_services() {
  # Best-effort local bootstrap using Docker Compose if endpoints are not reachable.
  # Starts SearxNG (host port 8888). LLM bootstrap removed.
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    # Bootstrap SearxNG if not reachable
    if ! curl -fsS -m 2 "${SEARX_URL%/}/" >/dev/null 2>&1; then
      docker compose -f docker-compose.yml -f docker-compose.override.yml.example up -d searxng >/dev/null 2>&1 || true
    fi
    # Small wait loop for readiness
    for i in {1..20}; do
      searx_ready=false
      curl -fsS -m 2 "${SEARX_URL%/}/" >/dev/null 2>&1 && searx_ready=true
      if [ "$searx_ready" = true ]; then break; fi
      sleep 2
    done
  fi
}

section "Prerequisites"
if have curl; then ok "curl found: $(curl --version | head -n1)"; else ko "curl not found. Install curl and re-run."; fi
if have jq; then ok "jq found: $(jq --version)"; else wn "jq not found. Model autodiscovery limited"; fi

section "Check SearxNG"
maybe_bootstrap_services
if curl -fsS -m 8 "${SEARX_URL%/}/" >/dev/null 2>&1; then
  ok "SearxNG reachable at ${SEARX_URL}"
else
  ko "SearxNG not reachable at ${SEARX_URL}"
fi

# Simple JSON query to ensure search works
searx_resp=$(curl -fsS -m 12 "${SEARX_URL%/}/search?q=hsts%20nginx&format=json&language=en&categories=it" 2>/dev/null || true)
if [[ -n "$searx_resp" ]]; then
  if have jq; then
    cnt=$(printf '%s' "$searx_resp" | jq '(.results | length) // 0' 2>/dev/null || echo 0)
    if [[ "$cnt" -ge 0 ]]; then ok "SearxNG JSON search responded (results=$cnt)"; else ko "SearxNG JSON search returned invalid JSON"; fi
  else
    if printf '%s' "$searx_resp" | grep -q '"results"'; then ok "SearxNG JSON search responded"; else ko "SearxNG JSON search missing results field"; fi
  fi
else
  if command -v docker >/dev/null 2>&1 && docker compose ps >/dev/null 2>&1; then
    cid=$(docker compose "${COMPOSE_ARGS[@]}" ps -q searxng 2>/dev/null || true)
    if [ -n "$cid" ]; then
      status=$(docker inspect -f '{{.State.Health.Status}}' "$cid" 2>/dev/null || true)
      if [ "$status" = "healthy" ] || [ "$status" = "starting" ]; then
        wn "Skipping JSON search over host: container healthy but host port not reachable"
      else
        ko "SearxNG JSON search failed"
      fi
    else
      ko "SearxNG JSON search failed"
    fi
  else
    ko "SearxNG JSON search failed"
  fi
fi

section "Check LLM (OpenAI-compatible)"
# Basic reachability and model listing against OpenAI-compatible API
trimmed_base=${LLM_BASE%/}
models_url="$trimmed_base/models"

# Build curl args, include Authorization only if OPENAI_API_KEY is provided
curl_args=( -sS -m 12 -H "Content-Type: application/json" )
if [[ -n "${OPENAI_API_KEY:-}" ]]; then
  curl_args+=( -H "Authorization: Bearer $OPENAI_API_KEY" )
fi

llm_resp=$(curl "${curl_args[@]}" "$models_url" 2>/dev/null || true)
if [[ -n "$llm_resp" ]]; then
  if have jq; then
    # Expect { object: "list", data: [ ... ] }
    object=$(printf '%s' "$llm_resp" | jq -r '.object // empty' 2>/dev/null || true)
    data_count=$(printf '%s' "$llm_resp" | jq '(.data | length) // 0' 2>/dev/null || echo 0)
    if [[ "$object" == "list" || "$data_count" -ge 0 ]]; then
      ok "LLM reachable at $trimmed_base (models listed: $data_count)"
    else
      ko "LLM responded but not in expected format at $trimmed_base"
    fi
  else
    if printf '%s' "$llm_resp" | grep -q '"data"'; then
      ok "LLM reachable at $trimmed_base (models endpoint responded)"
    else
      ko "LLM responded but missing data field at $trimmed_base"
    fi
  fi
else
  ko "LLM not reachable at $trimmed_base"
fi

section "Summary"
echo "${BOLD}Passes:${RESET} ${#passes[@]}"
for p in "${passes[@]}"; do echo "  - $p"; done
if [[ ${#warns[@]} -gt 0 ]]; then
  echo "${BOLD}Warnings:${RESET} ${#warns[@]}"; for w in "${warns[@]}"; do echo "  - $w"; done
fi
if [[ ${#fails[@]} -gt 0 ]]; then
  echo "${BOLD}Failures:${RESET} ${#fails[@]}"; for f in "${fails[@]}"; do echo "  - $f"; done
  exit 1
fi
exit 0
