#!/bin/bash
set -e
set -x

TIMEOUT_SEC=900       # 15 minutes
KILL_GRACE=30         # extra seconds before SIGKILL if still running

run_with_timeout() {
  local sec="$1"; shift

  # Prefer GNU coreutils timeout / gtimeout if present
  if command -v timeout >/dev/null 2>&1; then
    timeout -k "${KILL_GRACE}s" "${sec}s" "$@"
    return $?
  elif command -v gtimeout >/dev/null 2>&1; then
    gtimeout -k "${KILL_GRACE}s" "${sec}s" "$@"
    return $?
  fi

  # Portable Bash fallback: new process group + watchdog
  setsid "$@" &
  local pid=$!
  # Find its process group id
  local pgid
  pgid=$(ps -o pgid= -p "$pid" | tr -d ' ')

  (
    sleep "$sec"
    if kill -0 -"${pgid}" 2>/dev/null; then
      echo "Timeout after ${sec}s; terminating PGID ${pgid}" >&2
      kill -TERM -"${pgid}" 2>/dev/null
      sleep "$KILL_GRACE"
      kill -KILL -"${pgid}" 2>/dev/null || true
    fi
  ) &
  local watchdog=$!

  wait "$pid"
  local status=$?

  kill -TERM "$watchdog" 2>/dev/null || true
  wait "$watchdog" 2>/dev/null || true

  return "$status"
}

while grep -q '\* \[ \]' FEATURE_CHECKLIST.md; do

    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo
        echo "--- COMMITTING UNCHANGED TO GIT ---"
        echo
        git add .
        if ! run_with_timeout "$TIMEOUT_SEC" cursor-agent -p --output-format text -f -m gpt-5 \
            "Follow these rules .cursor/rules/go-commit.mdc"; then
            echo "cursor-agent commit step timed out or failed." >&2
        fi
    fi

    echo
    echo "--- WORKING ON ---"
    echo

    if ! run_with_timeout "$TIMEOUT_SEC" cursor-agent -p --output-format text -f -m gpt-5 \
        "Follow these rules .cursor/rules/go-implement.mdc .cursor/rules/go-dod.mdc .cursor/rules/go-diverse-tests.mdc .cursor/rules/go-work.mdc .cursor/rules/go-no-docker.mdc"; then
        echo "cursor-agent work step timed out or failed." >&2
    fi

    sleep 5
done

