#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"
status=0

require_file() {
  local path="$1"
  if [ ! -f "$ROOT_DIR/$path" ]; then
    echo "missing required file: $path" >&2
    status=1
  fi
}

require_target() {
  local path="$1"
  local target="$2"
  if ! awk -F: '/^[a-zA-Z0-9_.-]+:[^=]/{print $1}' "$ROOT_DIR/$path" | grep -qx "$target"; then
    echo "missing required target '$target' in $path" >&2
    status=1
  fi
}

PROJECT_ROOTS=()
for d in "$ROOT_DIR"/swobu*; do
  [ -d "$d" ] || continue
  PROJECT_ROOTS+=("$(basename "$d")")
done

if [ "${#PROJECT_ROOTS[@]}" -eq 0 ]; then
  echo "no project roots found (expected directories matching 'swobu*')" >&2
  exit 1
fi

for project in "${PROJECT_ROOTS[@]}"; do
  require_file "$project/Makefile"
  require_file "$project/README.md"
done

for project in "${PROJECT_ROOTS[@]}"; do
  has_top_readme=0
  has_second_readme=0
  if compgen -G "$ROOT_DIR/$project/*/README.md" >/dev/null; then
    has_top_readme=1
  fi
  if compgen -G "$ROOT_DIR/$project/*/*/README.md" >/dev/null; then
    has_second_readme=1
  fi
  if [ "$has_top_readme" -eq 0 ]; then
    echo "missing required README pattern: $project/*/README.md" >&2
    status=1
  fi
  if [ "$has_second_readme" -eq 0 ]; then
    echo "missing required README pattern: $project/*/*/README.md" >&2
    status=1
  fi
done

for mk in "${PROJECT_ROOTS[@]/%//Makefile}"; do
  require_target "$mk" "help"
  require_target "$mk" "verify"
done

if [ "$status" -ne 0 ]; then
  exit "$status"
fi

echo "project entrypoint checks passed"
