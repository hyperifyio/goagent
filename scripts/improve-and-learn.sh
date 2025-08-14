#!/bin/bash
set -e
#set -x

MODEL=gpt-5
OUTPUT_FORMAT=stream-json
TASK_TIMEOUT=60m
IMPROVE_MDC=.cursor/rules/improve-mdc.mdc

echo
echo "--- IMPROVING AND LEARNING ---"
echo

if timeout "$TASK_TIMEOUT" cursor-agent -p --output-format "$OUTPUT_FORMAT" -f -m "$MODEL" -- "$(cat "$IMPROVE_MDC")"; then
  echo
  echo '--- SUCCESSFUL END ---'
else
  ERRNO="$?"
  echo
  echo '--- ERROR:'"$ERRNO"' ---'
fi
echo
