#!/bin/bash
set -e
#set -x

MODEL=gpt-5
#OUTPUT_FORMAT=text
OUTPUT_FORMAT=stream-json
LOG_DIR=./logs
LOG_FILE="$LOG_DIR/coding.log"
IMPROVE_LOG_FILE="$LOG_DIR/improving.log"
COMMIT_TIMEOUT=15m
TASK_TIMEOUT=60m
COMMIT_MDC=.cursor/rules/go-commit.mdc
WORK_MDC=.cursor/rules/work-on-features.mdc
IMPROVE_MDC=.cursor/rules/improve-mdc.mdc

if test -f "$LOG_FILE"; then
  :
else
  mkdir -p "$LOG_DIR"
  touch "$LOG_FILE"
fi

while grep -q '\* \[ \]' FEATURE_CHECKLIST.md; do

    (
      echo
      echo -n 'DATE: '
      date -R
      echo -n 'UNFINISHED_TASKS: '
      grep -F '[ ] ' FEATURE_CHECKLIST.md | wc -l
      echo -n 'FINISHED_TASKS: '
      grep -F '[x] ' FEATURE_CHECKLIST.md | wc -l
      echo
    ) 2>&1 | tee -a "$LOG_FILE"

    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo 2>&1 | tee -a "$LOG_FILE"
        echo "--- COMMITTING UNCHANGED TO GIT ---" 2>&1 | tee -a "$LOG_FILE"
        echo 2>&1 | tee -a "$LOG_FILE"

        git add . 2>&1 | tee -a "$LOG_FILE"

        if timeout "$COMMIT_TIMEOUT" cursor-agent -p --output-format "$OUTPUT_FORMAT" -f -m "$MODEL" \
            -- "$(cat "$COMMIT_MDC")" \
          2>&1 | tee -a "$LOG_FILE"; then
          echo 2>&1 | tee -a "$LOG_FILE"
          echo '--- SUCCESSFUL END ---' 2>&1 | tee -a "$LOG_FILE"
          echo 2>&1 | tee -a "$LOG_FILE"
        else
          ERRNO="$?"
          echo 2>&1 | tee -a "$LOG_FILE"
          echo '--- ERROR:'"$ERRNO"' ---' 2>&1 | tee -a "$LOG_FILE"
          echo 2>&1 | tee -a "$LOG_FILE"
        fi

    fi

    echo 2>&1 | tee -a "$LOG_FILE"
    echo "--- WORKING ON ---" 2>&1 | tee -a "$LOG_FILE"
    echo 2>&1 | tee -a "$LOG_FILE"

    if timeout "$TASK_TIMEOUT" cursor-agent -p --output-format "$OUTPUT_FORMAT" -f -m "$MODEL" -- "$(cat "$WORK_MDC")" \
      2>&1 | tee -a "$LOG_FILE"; then
      echo 2>&1 | tee -a "$LOG_FILE"
      echo '--- SUCCESSFUL END ---' 2>&1 | tee -a "$LOG_FILE"
      echo 2>&1 | tee -a "$LOG_FILE"
    else
      ERRNO="$?"
      echo 2>&1 | tee -a "$LOG_FILE"
      echo '--- ERROR:'"$ERRNO"' ---' 2>&1 | tee -a "$LOG_FILE"
      echo 2>&1 | tee -a "$LOG_FILE"
    fi

    echo 2>&1 | tee -a "$IMPROVE_LOG_FILE"
    echo "--- IMPROVING AND LEARNING ---" 2>&1 | tee -a "$IMPROVE_LOG_FILE"
    echo 2>&1 | tee -a "$IMPROVE_LOG_FILE"

    if timeout "$TASK_TIMEOUT" cursor-agent -p --output-format "$OUTPUT_FORMAT" -f -m "$MODEL" -- "$(cat "$IMPROVE_MDC")" \
      2>&1 | tee -a "$IMPROVE_LOG_FILE"; then
      echo 2>&1 | tee -a "$IMPROVE_LOG_FILE"
      echo '--- SUCCESSFUL END ---' 2>&1 | tee -a "$IMPROVE_LOG_FILE"
      echo 2>&1 | tee -a "$IMPROVE_LOG_FILE"
    else
      ERRNO="$?"
      echo 2>&1 | tee -a "$IMPROVE_LOG_FILE"
      echo '--- ERROR:'"$ERRNO"' ---' 2>&1 | tee -a "$IMPROVE_LOG_FILE"
      echo 2>&1 | tee -a "$IMPROVE_LOG_FILE"
    fi

    sleep 5
done
