#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CLI_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

args=("$@")

if [[ -n "${SWOBU_OPENROUTER_KEY_FILE:-}" ]]; then
  has_key_flag=0
  for arg in "${args[@]}"; do
    if [[ "$arg" == "-openrouter-key-file" ]] || [[ "$arg" == -openrouter-key-file=* ]]; then
      has_key_flag=1
      break
    fi
  done
  if [[ "$has_key_flag" -eq 0 ]]; then
    args=("-openrouter-key-file" "${SWOBU_OPENROUTER_KEY_FILE}" "${args[@]}")
  fi
fi

if [[ -n "${SWOBU_OPENAI_KEY_FILE:-}" ]]; then
  has_key_flag=0
  for arg in "${args[@]}"; do
    if [[ "$arg" == "-openai-key-file" ]] || [[ "$arg" == -openai-key-file=* ]]; then
      has_key_flag=1
      break
    fi
  done
  if [[ "$has_key_flag" -eq 0 ]]; then
    args=("-openai-key-file" "${SWOBU_OPENAI_KEY_FILE}" "${args[@]}")
  fi
fi

if [[ -n "${SWOBU_ANTHROPIC_KEY_FILE:-}" ]]; then
  has_key_flag=0
  for arg in "${args[@]}"; do
    if [[ "$arg" == "-anthropic-key-file" ]] || [[ "$arg" == -anthropic-key-file=* ]]; then
      has_key_flag=1
      break
    fi
  done
  if [[ "$has_key_flag" -eq 0 ]]; then
    args=("-anthropic-key-file" "${SWOBU_ANTHROPIC_KEY_FILE}" "${args[@]}")
  fi
fi

cd "$CLI_ROOT"
exec go run ./internal/devtools/cmd/liveevidencemine "${args[@]}"
