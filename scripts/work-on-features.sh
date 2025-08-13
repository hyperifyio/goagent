#!/bin/bash
set -e
set -x

LOG_DIR=./logs
LOG_FILE="$LOG_DIR/coding.log"

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

        if cursor-agent -p --output-format text -f -m gpt-5 \
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

    if timeout 60m cursor-agent -p --output-format text -f -m gpt-5 \
        "You have OpenAI compatible LLM available with supported end points GET /v1/models, POST /v1/chat/completions, POST /v1/completions, and POST /v1/embeddings -- at http://localhost:1234 with model openai/gpt-oss-20b. "  \
        "You have full access to shell commands like 'git'. You MUST system test everything with real services. " \
        "Follow instructions .cursor/rules/go-implement.mdc .cursor/rules/go-dod.mdc .cursor/rules/go-diverse-tests.mdc .cursor/rules/go-work.mdc. " \
        "Before you start working on a new task: " \
        " - investigate the current project and check out any unchecked features which have already been completely done in FEATURE_CHECKLIST.md. To check off a feature, rewrite it to '* [x] ...'" \
        " - investigate the previous history from log: $LOG_FILE" \
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
