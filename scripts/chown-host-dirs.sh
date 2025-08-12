#!/usr/bin/env bash
set -euo pipefail

# Traceability: Implements FEATURE_CHECKLIST.md item 211 (Non-root volumes & permissions)
# Usage:
#   APP_UID=${APP_UID:-$(id -u)}
#   APP_GID=${APP_GID:-$(id -g)}
#   ./scripts/chown-host-dirs.sh [paths...]
# If no paths are given, defaults to .goresearch-cache and reports.

APP_UID=${APP_UID:-$(id -u)}
APP_GID=${APP_GID:-$(id -g)}

paths=("${@}")
if [ ${#paths[@]} -eq 0 ]; then
  paths=(".goresearch-cache" "reports")
fi

for p in "${paths[@]}"; do
  if [ -e "$p" ]; then
    echo "chown -R ${APP_UID}:${APP_GID} $p"
    chown -R "${APP_UID}:${APP_GID}" "$p"
  else
    echo "skip (missing): $p"
  fi
done
