#!/usr/bin/env bash
# Every `uses: owner/repo@<sha>` in the workflows must point at a commit that
# exists (a commit, or an annotated tag object): a mistyped pin only fails at
# the first run of the job, which for a release workflow is the release itself. Local actions (./...) and docker://
# references are not pins. Needs gh with a token that can read public repos.
set -euo pipefail
dir="${1:-.github/workflows}"
status=0
while read -r ref; do
  path="${ref%@*}"; sha="${ref##*@}"
  repo="$(printf '%s' "$path" | cut -d/ -f1-2)"
  # A pin may name an annotated tag object rather than a commit: Actions
  # accepts both, the commits endpoint only the latter.
  if gh api "repos/$repo/commits/$sha" --jq .sha >/dev/null 2>&1 \
    || gh api "repos/$repo/git/tags/$sha" --jq .sha >/dev/null 2>&1; then
    echo "ok   $ref"
  else
    echo "::error::$ref: commit $sha not found in $repo"
    status=1
  fi
done < <(grep -rhoE 'uses:[[:space:]]*[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(/[A-Za-z0-9_./-]+)?@[0-9a-f]{40}' "$dir" \
  | sed -E 's/^uses:[[:space:]]*//' | sort -u)
exit "$status"
