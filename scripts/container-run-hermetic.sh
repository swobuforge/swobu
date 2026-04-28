#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 0 ]]; then
  echo "usage: swobucli/scripts/container-run-hermetic.sh '<command>'" >&2
  echo "env: HERMETIC_CONTAINER_REBUILD=1 to force image rebuild" >&2
  echo "env: HERMETIC_CONTAINER_CACHE_PREFIX=<name> to isolate cache volumes" >&2
  exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CLI_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$CLI_ROOT/.." && pwd)"

runtime="$("$SCRIPT_DIR/detect-container-runtime.sh")"
image="${HERMETIC_CONTAINER_IMAGE:-${VERIFY_CONTAINER_IMAGE:-swobu-hermetic-local}}"
containerfile="${HERMETIC_CONTAINERFILE:-${VERIFY_CONTAINERFILE:-$CLI_ROOT/test/container/hermetic/Containerfile}}"
cache_prefix="${HERMETIC_CONTAINER_CACHE_PREFIX:-${VERIFY_CONTAINER_CACHE_PREFIX:-swobu-hermetic}}"
rebuild="${HERMETIC_CONTAINER_REBUILD:-${VERIFY_CONTAINER_REBUILD:-0}}"

if [[ "$rebuild" == "1" ]] || ! "$runtime" image inspect "$image" >/dev/null 2>&1; then
  "$runtime" build -f "$containerfile" -t "$image" "$REPO_ROOT"
fi

"$runtime" run --rm \
  -v "${REPO_ROOT}:/workspace" \
  -v "${cache_prefix}-gomod:/go/pkg/mod" \
  -v "${cache_prefix}-gobuild:/root/.cache/go-build" \
  -v "${cache_prefix}-golangci:/root/.cache/golangci-lint" \
  -w /workspace/swobucli \
  -e GOMODCACHE=/go/pkg/mod \
  -e GOCACHE=/root/.cache/go-build \
  -e GOLANGCI_LINT_CACHE=/root/.cache/golangci-lint \
  -e SWOBU_HERMETIC_CONTAINER=1 \
  -e TERM=xterm-256color \
  -e PATH=/root/.local/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  "$image" \
  bash -c "$*"
