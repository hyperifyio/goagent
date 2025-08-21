#!/usr/bin/env bash
set -euo pipefail

# pr-dedupe.sh
# Detect and de-duplicate overlapping or redundant pull requests by scanning open PRs,
# comparing commits and diffs, and closing true duplicates with a transparent audit trail.
#
# Requirements:
# - GitHub CLI (gh) authenticated with repo scope
# - jq
#
# Usage:
#   hack/pr-dedupe.sh [--apply] [--owner OWNER] [--repo REPO] [--assignee <login>] [--limit N]
#
# Modes:
# - Default is dry-run: prints a detailed report only
# - --apply will perform safe actions: close exact/commit-equivalent non-canonical PRs with rationale,
#   migrate labels, unassign from duplicates if assigned, and post cross-references.
#
# Notes:
# - Never force-pushes or deletes branches
# - Avoids closing near-duplicates; proposes consolidation instead
# - Skips intentional backports to different base branches

APPLY=false
OWNER_REPO=""
ASSIGNEE_FILTER=""
LIMIT="100"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply)
      APPLY=true; shift ;;
    --owner)
      OWNER="$2"; shift 2 ;;
    --repo)
      REPO="$2"; shift 2 ;;
    --assignee)
      ASSIGNEE_FILTER="$2"; shift 2 ;;
    --limit)
      LIMIT="$2"; shift 2 ;;
    -h|--help)
      sed -n '1,80p' "$0"; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [[ -n "${OWNER:-}" && -n "${REPO:-}" ]]; then
  OWNER_REPO="$OWNER/$REPO"
fi

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "Missing dependency: $1" >&2; exit 1; }; }
require_cmd gh
require_cmd jq

info() { echo "[info] $*"; }
warn() { echo "[warn] $*" >&2; }
act()  { $APPLY && echo "[apply] $*" || echo "[dryrun] $*"; }

# Resolve default repo from current directory if not passed
if [[ -z "$OWNER_REPO" ]]; then
  OWNER_REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
fi

info "Repository: $OWNER_REPO"

# 1) List open PRs with essential fields
info "Fetching open PRs..."
PRS_JSON=$(gh pr list --repo "$OWNER_REPO" --state open --limit "$LIMIT" \
  --json number,title,author,state,isDraft,baseRefName,headRefName,labels,assignees)

# 2) For each PR, enrich with commits and patch/diff identifiers
pr_numbers=$(echo "$PRS_JSON" | jq -r '.[].number')

if [[ -z "$pr_numbers" ]]; then
  info "No open PRs."; exit 0
fi

# Helper to fetch details per PR
fetch_pr_details() {
  local pr="$1"
  local base head author title draft
  base=$(echo "$PRS_JSON" | jq -r --arg pr "$pr" '.[] | select(.number==($pr|tonumber)) | .baseRefName')
  head=$(echo "$PRS_JSON" | jq -r --arg pr "$pr" '.[] | select(.number==($pr|tonumber)) | .headRefName')
  author=$(echo "$PRS_JSON" | jq -r --arg pr "$pr" '.[] | select(.number==($pr|tonumber)) | .author.login')
  title=$(echo "$PRS_JSON" | jq -r --arg pr "$pr" '.[] | select(.number==($pr|tonumber)) | .title')
  draft=$(echo "$PRS_JSON" | jq -r --arg pr "$pr" '.[] | select(.number==($pr|tonumber)) | .isDraft')

  # Commits SHAs and tree-ish comparison
  commits=$(gh pr view "$pr" --repo "$OWNER_REPO" --json commits -q '.commits[].oid')
  # Unified diff; the output is deterministic for identical changes
  diff=$(gh pr diff "$pr" --repo "$OWNER_REPO" --patch)
  # File list for coarse similarity
  files=$(gh pr view "$pr" --repo "$OWNER_REPO" --json files -q '.files[].path' | sort | tr '\n' ',')

  # Hashes for grouping
  diff_hash=$(printf "%s" "$diff" | sha256sum | awk '{print $1}')
  commits_hash=$(printf "%s" "$commits" | sha256sum | awk '{print $1}')
  files_hash=$(printf "%s" "$files" | sha256sum | awk '{print $1}')

  echo "{\"number\":$pr,\"base\":\"$base\",\"head\":\"$head\",\"author\":\"$author\",\"title\":\"$(echo "$title" | sed 's/"/\\"/g')\",\"draft\":$draft,\"diff_hash\":\"$diff_hash\",\"commits_hash\":\"$commits_hash\",\"files_hash\":\"$files_hash\"}"
}

info "Computing diff and commit signatures..."
DETAILS_JSON="["
first=true
for pr in $pr_numbers; do
  j=$(fetch_pr_details "$pr")
  if $first; then
    DETAILS_JSON+="$j"; first=false
  else
    DETAILS_JSON+=" , $j"
  fi
done
DETAILS_JSON+="]"

# 3) Detect duplicate clusters
# Exact duplicates: same base AND same diff_hash
# Commit-equivalent: same base AND same commits_hash (but diff_hash may differ by metadata)
# Near-duplicates: same base AND same files_hash but different diff/commits hash

jq_prog='
  {
    exact: (sort_by(.base, .diff_hash)
            | group_by([.base, .diff_hash])
            | map({key: (.[0].base+":"+.[0].diff_hash), prs: map(.number)})),
    commit_equiv: (sort_by(.base, .commits_hash)
                   | group_by([.base, .commits_hash])
                   | map({key: (.[0].base+":"+.[0].commits_hash), prs: map(.number)})),
    near: (sort_by(.base, .files_hash)
           | group_by([.base, .files_hash])
           | map({key: (.[0].base+":"+.[0].files_hash), prs: map(.number)}))
  }'

CLUSTERS=$(echo "$DETAILS_JSON" | jq "$jq_prog")

# Helper to pick canonical PR among a set, using gh api for reviews and checks
pick_canonical() {
  local prs_csv="$1"
  IFS=',' read -r -a arr <<< "$prs_csv"
  # Collect metadata
  local best=""
  local best_score=-1
  for pr in "${arr[@]}"; do
    pr=${pr// /}
    # Fetch more context
    view_json=$(gh pr view "$pr" --repo "$OWNER_REPO" --json number,createdAt,isDraft,checksStatus,reviewDecision,labels,linkedIssues,assignees)
    created=$(echo "$view_json" | jq -r .createdAt)
    draft=$(echo "$view_json" | jq -r .isDraft)
    checks=$(echo "$view_json" | jq -r .checksStatus)
    review=$(echo "$view_json" | jq -r .reviewDecision)
    has_issue=$(echo "$view_json" | jq -r '.linkedIssues | length > 0')

    # Score: non-draft(2) + checks pass(3) + review approved(2) + linked issue(1) + older(creation time inverse)
    score=0
    [[ "$draft" == "false" ]] && score=$((score+2))
    [[ "$checks" == "SUCCESS" ]] && score=$((score+3))
    [[ "$review" == "APPROVED" ]] && score=$((score+2))
    [[ "$has_issue" == "true" ]] && score=$((score+1))
    # Age bonus: earlier is better
    ts=$(date -d "$created" +%s || date -j -f "%Y-%m-%dT%H:%M:%SZ" "$created" +%s 2>/dev/null || echo 0)
    score=$((score + (ts/100000)))

    if (( score > best_score )); then
      best=$pr; best_score=$score
    elif (( score == best_score )); then
      # tie-breaker: lowest PR number
      if (( pr < best )); then best=$pr; fi
    fi
  done
  echo "$best"
}

join_by_comma() { local IFS=","; echo "$*"; }

# Collate clusters with 2+ PRs
extract_clusters() {
  local kind="$1"; local jq_path="$2"
  echo "$CLUSTERS" | jq -r "$jq_path | map(select(.prs|length>1)) | to_entries[] | (\"$kind\" + \"|\" + (.value.prs | join(\",\")))"
}

exact_clusters=$(extract_clusters "exact" '.exact')
commit_clusters=$(extract_clusters "commit" '.commit_equiv')
near_clusters=$(extract_clusters "near" '.near')

report_line() {
  local kind="$1"; local prs_csv="$2"
  canonical=$(pick_canonical "$prs_csv")
  echo "$kind|canonical:$canonical|prs:$prs_csv"
}

REPORT=""

# Build a set of PRs already covered by exact/commit clusters to avoid double-processing in near
declare -A HANDLED
while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  kind=${line%%|*}
  prs_csv=${line#*|}; prs_csv=${prs_csv#*|}; prs_csv=${prs_csv#prs:}
  # Only consider exact/commit here
  if [[ "$kind" != "near" ]]; then
    IFS=',' read -r -a arr <<< "$prs_csv"
    for p in "${arr[@]}"; do p=${p// /}; [[ -z "$p" ]] && continue; HANDLED[$p]=1; done
  fi
  REPORT+=$(report_line "$kind" "$prs_csv"); REPORT+=$'\n'
done <<< "$exact_clusters"

while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  kind=${line%%|*}
  prs_csv=${line#*|}; prs_csv=${prs_csv#*|}; prs_csv=${prs_csv#prs:}
  IFS=',' read -r -a arr <<< "$prs_csv"
  for p in "${arr[@]}"; do p=${p// /}; [[ -z "$p" ]] && continue; HANDLED[$p]=1; done
  REPORT+=$(report_line "$kind" "$prs_csv"); REPORT+=$'\n'
done <<< "$commit_clusters"

# Near clusters: filter out PRs already handled; only include clusters with 2+ remaining PRs
while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  kind=${line%%|*}
  prs_csv=${line#*|}; prs_csv=${prs_csv#*|}; prs_csv=${prs_csv#prs:}
  IFS=',' read -r -a arr <<< "$prs_csv"
  filtered=()
  for p in "${arr[@]}"; do p=${p// /}; [[ -z "$p" ]] && continue; [[ -n "${HANDLED[$p]:-}" ]] && continue; filtered+=("$p"); done
  if (( ${#filtered[@]} >= 2 )); then
    new_csv=$(IFS=","; echo "${filtered[*]}")
    REPORT+=$(report_line "$kind" "$new_csv"); REPORT+=$'\n'
  fi
done <<< "$near_clusters"

if [[ -z "$REPORT" ]]; then
  info "No duplicate clusters detected."
  exit 0
fi

printf "Duplicate clusters detected (kind|canonical|prs):\n%s" "$REPORT"

# Apply actions
if $APPLY; then
  while IFS= read -r entry; do
    [[ -z "$entry" ]] && continue
    kind=${entry%%|*}
    rest=${entry#*|}
    canonical=${rest#canonical:}; canonical=${canonical%%|*}
    prs_csv=${entry##*prs:}

    IFS=',' read -r -a prs <<< "$prs_csv"
    for pr in "${prs[@]}"; do
      pr=${pr// /}
      [[ "$pr" == "$canonical" ]] && continue

      # Respect assignee filter when provided: only act on PRs authored by or assigned to login
      if [[ -n "$ASSIGNEE_FILTER" ]]; then
        pr_view=$(gh pr view "$pr" --repo "$OWNER_REPO" --json author,assignees)
        pr_author=$(echo "$pr_view" | jq -r .author.login)
        is_assignee=$(echo "$pr_view" | jq -r --arg a "$ASSIGNEE_FILTER" '.assignees[].login == $a' 2>/dev/null | grep -q true && echo yes || echo no)
        if [[ "$pr_author" != "$ASSIGNEE_FILTER" && "$is_assignee" != yes ]]; then
          act "Skipping PR #$pr (not authored by or assigned to $ASSIGNEE_FILTER)"
          continue
        fi
      fi

      if [[ "$kind" == "near" ]]; then
        # Convert to draft with guidance
        act "Converting PR #$pr to draft due to overlap with #$canonical"
        gh pr ready "$pr" --undo --repo "$OWNER_REPO" || true
        note="This PR substantially overlaps with #$canonical (near-duplicate detected by file set overlap). Consider rebasing onto the canonical branch, reducing the diff to unique changes, or merging unique commits into #$canonical."
        gh pr comment "$pr" --repo "$OWNER_REPO" --body "$note"
        gh pr comment "$canonical" --repo "$OWNER_REPO" --body "Context from #$pr: $note"
        continue
      fi

      # For exact or commit-equivalent duplicates, close non-canonical PRs
      act "Closing PR #$pr as duplicate of #$canonical"

      # Migrate labels that carry process meaning
      labels=$(gh pr view "$pr" --repo "$OWNER_REPO" --json labels -q '.labels[].name' | tr '\n' ',')
      if [[ -n "$labels" ]]; then
        IFS=',' read -r -a labs <<< "$labels"
        for lb in "${labs[@]}"; do
          [[ -z "$lb" ]] && continue
          gh pr edit "$canonical" --repo "$OWNER_REPO" --add-label "$lb" || true
        done
      fi

      rationale="Closing in favor of #$canonical. Detected $kind-duplicate via $( [[ "$kind" == "exact" ]] && echo "identical diff" || echo "identical commit set"). Discussion remains preserved; no unique commits are lost."
      gh pr close "$pr" --repo "$OWNER_REPO" --comment "$rationale"

      # Cross-reference
      gh pr comment "$canonical" --repo "$OWNER_REPO" --body "Supersedes #$pr ($kind duplicate)."

      # Assignment adjustments when filter is provided: remain assigned to canonical, unassign from duplicate
      if [[ -n "$ASSIGNEE_FILTER" ]]; then
        gh pr edit "$canonical" --repo "$OWNER_REPO" --add-assignee "$ASSIGNEE_FILTER" || true
        gh pr edit "$pr" --repo "$OWNER_REPO" --remove-assignee "$ASSIGNEE_FILTER" || true
      fi
    done
  done <<< "$REPORT"
fi
