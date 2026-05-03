#!/usr/bin/env sh
set -eu

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "verify-published failed: required command not found: $1" >&2
    exit 1
  fi
}

need_cmd gh
need_cmd sh

repo="swobuforge/swobu"

# Verify latest release exists using authenticated GitHub API so private repos work.
latest_tag="$(gh release view --repo "$repo" --json tagName --jq .tagName)"
if [ -z "$latest_tag" ]; then
  echo "verify-published failed: latest release tag is empty" >&2
  exit 1
fi

# Verify installer script exists on main branch.
gh api "repos/$repo/contents/scripts/install.sh?ref=main" >/dev/null

# Exercise installer dry-run by fetching script content through authenticated API.
installer_body="$(
  gh api \
    -H "Accept: application/vnd.github.raw" \
    "repos/$repo/contents/scripts/install.sh?ref=main"
)"

dry_run_output="$(printf '%s\n' "$installer_body" | VERSION="$latest_tag" DRY_RUN=true sh)"
printf '%s' "$dry_run_output" | grep -q '^tag=v'
printf '%s' "$dry_run_output" | grep -q '^archive_url=https://github.com/swobuforge/swobu/releases/download/v'
printf '%s' "$dry_run_output" | grep -q '^checksums_url=https://github.com/swobuforge/swobu/releases/download/v'

echo "verify-published OK"
