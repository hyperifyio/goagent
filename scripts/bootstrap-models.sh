#!/usr/bin/env bash
set -euo pipefail

# Bootstrap script for preparing model weights in a shared volume.
# Usage: scripts/bootstrap-models.sh [MODELS_DIR] [CHECKSUM_FILE]
# If CHECKSUM_FILE is omitted, expects MODELS_DIR/checksums.sha256

MODELS_DIR="${1:-models}"
CHECKSUM_FILE="${2:-$MODELS_DIR/checksums.sha256}"

if [[ -f "$CHECKSUM_FILE" ]]; then
  echo "Verifying model checksums in $(dirname "$CHECKSUM_FILE")..."
  ( cd "$(dirname "$CHECKSUM_FILE")" && sha256sum -c "$(basename "$CHECKSUM_FILE")" )
  echo "Checksum verification passed."
else
  echo "No checksum file found at $CHECKSUM_FILE; skipping verification."
fi
