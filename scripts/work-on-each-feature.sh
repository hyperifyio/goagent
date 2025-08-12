#!/bin/bash
set -euo pipefail
set -x

#choices=("gpt-5" "sonnet-4" "opus-4.1")
choices=("gpt-5")

FEATURE_FILE="FEATURE_CHECKLIST.md"

commit_if_changes() {
  if ! git diff --quiet || ! git diff --cached --quiet; then

    # Randomize model
    MODEL="${choices[RANDOM % ${#choices[@]}]}"
    echo
    echo "MODEL=$MODEL"
    echo
    echo "--- COMMITTING LOCAL CHANGES ---"
    echo
    git add .
    timeout 5m cursor-agent -p --output-format text -f -m "$MODEL" \
      "Follow these rules .cursor/rules/go-commit.mdc"
  fi
}

# Portable shuffler (works on macOS/BSD + Linux without gshuf)
shuffle_lines() {
  awk 'BEGIN{srand()} {printf "%f\t%s\n", rand(), $0}' \
  | sort -k1,1 \
  | cut -f2-
}

while grep -Eq '^\s*\*\s\[\s\]' "$FEATURE_FILE"; do
  commit_if_changes

  # Build a randomized list of current unchecked tasks (task text only)
  tasks=()
  while IFS= read -r t; do
    tasks+=("$t")
  done < <(
    grep -nE '^\s*\*\s\[\s\]\s' "$FEATURE_FILE" \
    | shuffle_lines \
    | sed -E 's/^[[:space:]]*[0-9]+:[[:space:]]*\*\s\[\s\]\s*//'
  )

  # Work through the shuffled tasks
  for task_text in "${tasks[@]}"; do
    echo
    echo "--- WORKING ON: $task_text ---"
    echo

    prompt=$(cat <<EOF
You have full access to shell commands like 'git' and 'docker'. You MUST system test everything with real services, and expect existing implementations not been finished and require fixing.

Follow these rules .cursor/rules/go-implement.mdc .cursor/rules/go-dod.mdc .cursor/rules/go-diverse-tests.mdc .cursor/rules/go-work.mdc

Implement the following single FEATURE_CHECKLIST item now — focus ONLY on this item:

$task_text

When implementation and tests are complete, update FEATURE_CHECKLIST.md to check off this exact line. Keep commits small and meaningful.
EOF
)

    # Randomize model
    MODEL="${choices[RANDOM % ${#choices[@]}]}"
    echo "MODEL=$MODEL"
    echo

    if timeout 60m cursor-agent -p --output-format text -f -m "$MODEL" "$prompt"; then
      echo
      echo '--- SUCCESS ---'
      echo
    else
      echo
      echo '--- FAILURE ---'
      echo
    fi

    # Commit after each attempt
    commit_if_changes
    sleep 5
  done
done

echo "All checklist items completed (no unchecked tasks remain)."

