#!/bin/bash
set -e
set -x
while grep -q '\* \[ \]' FEATURE_CHECKLIST.md; do

    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo
        echo "--- COMMITTING UNCHANGED TO GIT ---"
        echo
        git add .
        cursor-agent -p --output-format text -f -m gpt-5 \
            "Follor these rules .cursor/rules/go-commit.mdc"
    fi

    echo
    echo "--- WORKING ON ---"
    echo

    timeout 60m cursor-agent -p --output-format text -f -m gpt-5 \
        "You have OpenAI compatible LLM available with supported end points GET /v1/models, POST /v1/chat/completions, POST /v1/completions, and POST /v1/embeddings -- at http://localhost:1234 with model openai/gpt-oss-20b. You have full access to shell commands like 'git'. You MUST system test everything with real services. Follow these rules .cursor/rules/go-implement.mdc .cursor/rules/go-dod.mdc .cursor/rules/go-diverse-tests.mdc .cursor/rules/go-work.mdc"

    sleep 5
done
