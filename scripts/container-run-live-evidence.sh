#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 0 ]]; then
  echo "usage: swobucli/scripts/container-run-live-evidence.sh '<command>'" >&2
  echo "env: LIVE_MINING_CONTAINER_REBUILD=1 to force image rebuild" >&2
  echo "env: LIVE_MINING_CONTAINER_CACHE_PREFIX=<name> to isolate cache volumes" >&2
  exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CLI_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$CLI_ROOT/.." && pwd)"

runtime="$("$SCRIPT_DIR/detect-container-runtime.sh")"
image="${LIVE_MINING_CONTAINER_IMAGE:-${HERMETIC_CONTAINER_IMAGE:-swobu-hermetic-local}}"
containerfile="${LIVE_MINING_CONTAINERFILE:-${HERMETIC_CONTAINERFILE:-$CLI_ROOT/test/container/hermetic/Containerfile}}"
cache_prefix="${LIVE_MINING_CONTAINER_CACHE_PREFIX:-swobu-hermetic}"
rebuild="${LIVE_MINING_CONTAINER_REBUILD:-0}"

if [[ "$rebuild" == "1" ]] || ! "$runtime" image inspect "$image" >/dev/null 2>&1; then
  "$runtime" build -f "$containerfile" -t "$image" "$REPO_ROOT"
fi

key_file="${SWOBU_OPENROUTER_KEY_FILE:-$CLI_ROOT/.secrets/openrouter.key}"
container_key_file="/run/swobu/openrouter.key"
openai_key_file="${SWOBU_OPENAI_KEY_FILE:-$CLI_ROOT/.secrets/openai.key}"
container_openai_key_file="/run/swobu/openai.key"
anthropic_key_file="${SWOBU_ANTHROPIC_KEY_FILE:-$CLI_ROOT/.secrets/anthropic.key}"
container_anthropic_key_file="/run/swobu/anthropic.key"

run_args=(
  --rm
  -v "${REPO_ROOT}:/workspace"
  -v "${cache_prefix}-gomod:/go/pkg/mod"
  -v "${cache_prefix}-gobuild:/root/.cache/go-build"
  -w /workspace/swobucli
  -e GOMODCACHE=/go/pkg/mod
  -e GOCACHE=/root/.cache/go-build
  -e PATH=/root/.local/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  -e TERM=xterm-256color
)

if [[ -n "${OPENROUTER_API_KEY:-}" ]]; then
  run_args+=( -e "OPENROUTER_API_KEY=${OPENROUTER_API_KEY}" )
elif [[ -f "$key_file" ]]; then
  run_args+=(
    -v "${key_file}:${container_key_file}:ro"
    -e "SWOBU_OPENROUTER_KEY_FILE=${container_key_file}"
  )
else
  echo "missing credentials: set OPENROUTER_API_KEY or provide SWOBU_OPENROUTER_KEY_FILE (default .secrets/openrouter.key)" >&2
  exit 1
fi

if [[ -n "${OPENROUTER_MODEL:-}" ]]; then
  run_args+=( -e "OPENROUTER_MODEL=${OPENROUTER_MODEL}" )
fi

if [[ -n "${OPENAI_API_KEY:-}" ]]; then
  run_args+=( -e "OPENAI_API_KEY=${OPENAI_API_KEY}" )
elif [[ -f "$openai_key_file" ]]; then
  run_args+=(
    -v "${openai_key_file}:${container_openai_key_file}:ro"
    -e "SWOBU_OPENAI_KEY_FILE=${container_openai_key_file}"
  )
fi

if [[ -n "${OPENAI_MODEL:-}" ]]; then
  run_args+=( -e "OPENAI_MODEL=${OPENAI_MODEL}" )
fi

if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
  run_args+=( -e "ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}" )
elif [[ -f "$anthropic_key_file" ]]; then
  run_args+=(
    -v "${anthropic_key_file}:${container_anthropic_key_file}:ro"
    -e "SWOBU_ANTHROPIC_KEY_FILE=${container_anthropic_key_file}"
  )
elif [[ -f "$CLI_ROOT/.secrets/claude.key" ]]; then
  run_args+=(
    -v "$CLI_ROOT/.secrets/claude.key:${container_anthropic_key_file}:ro"
    -e "SWOBU_ANTHROPIC_KEY_FILE=${container_anthropic_key_file}"
  )
fi

"$runtime" run "${run_args[@]}" "$image" bash -c "$*"
