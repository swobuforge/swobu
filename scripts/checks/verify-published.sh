#!/usr/bin/env sh
set -eu

release_latest_url="https://github.com/swobuforge/swobu/releases/latest"
installer_url="https://raw.githubusercontent.com/swobuforge/swobu/main/scripts/install.sh"

curl -fsSIL --max-time 10 "$release_latest_url" >/dev/null
curl -fsSIL --max-time 10 "$installer_url" >/dev/null

dry_run_output="$(curl -fsSL --max-time 20 "$installer_url" | DRY_RUN=true sh)"
printf '%s' "$dry_run_output" | grep -q '^tag=v'
printf '%s' "$dry_run_output" | grep -q '^archive_url=https://github.com/swobuforge/swobu/releases/download/v'
printf '%s' "$dry_run_output" | grep -q '^checksums_url=https://github.com/swobuforge/swobu/releases/download/v'

echo "verify-published OK"
