#!/bin/bash
set -e
#set -x

MODEL=gpt-5
#OUTPUT_FORMAT=text
OUTPUT_FORMAT=stream-json
LOG_DIR=./logs
LOG_FILE="$LOG_DIR/coding.log"
COMMIT_TIMEOUT=15m
TASK_TIMEOUT=60m

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
            "Follor these rules .cursor/rules/go-commit.mdc" \
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

    if timeout "$TASK_TIMEOUT" cursor-agent -p --output-format "$OUTPUT_FORMAT" -f -m "$MODEL" \
        "You have OpenAI compatible LLM available with supported end points GET /v1/models, POST /v1/chat/completions, POST /v1/completions, and POST /v1/embeddings -- at http://localhost:1234 with model openai/gpt-oss-20b. "  \
        "You have full access to shell commands like 'git'. You MUST system test everything with real services. " \
        "Follow instructions from .cursor/rules/go-implement.mdc .cursor/rules/go-dod.mdc .cursor/rules/go-diverse-tests.mdc AND .cursor/rules/go-work.mdc. " \
        "Before you start working on a new task: " \
        " - investigate the current project and check out any unchecked features which have already been completely done in FEATURE_CHECKLIST.md. To check off a feature, rewrite it to '* [x] ...'" \
        " - investigate the previous history from log file $LOG_FILE to know where you left the work last time." \
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

    sleep 5
done
