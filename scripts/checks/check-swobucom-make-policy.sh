#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../../.." && pwd)"
SWOBUCOM_MAKEFILE="$ROOT_DIR/swobucom/Makefile"
SITE_MAKEFILE="$ROOT_DIR/swobucom/site/Makefile"

fail() {
  echo "make policy failed: $1" >&2
  exit 1
}

root_allowed_pattern='^(help|dev([-.].+)?|lint([-.].+)?|test([-.].+)?|build([-.].+)?|verify([-.].+)?|check([-.].+)?|run([-.].+)?|clean([-.].+)?|site-(dev|lint|test|build|verify|check|run|clean)([-.].+)?|api-(dev|lint|test|build|verify|check|run|clean)([-.].+)?)$'
surface_allowed_pattern='^(help|dev([-.].+)?|lint([-.].+)?|test([-.].+)?|build([-.].+)?|verify([-.].+)?|check([-.].+)?|run([-.].+)?|clean([-.].+)?)$'

check_public_targets() {
  local makefile="$1"
  local label="$2"
  local allowed_pattern="$3"
  while IFS= read -r target; do
    if [[ ! "$target" =~ $allowed_pattern ]]; then
      fail "$label has non-idiomatic public target: $target"
    fi
    if [[ "$target" == *firstscreen* || "$target" == *fullpage* || "$target" == *full-page* ]]; then
      fail "$label scenario-specific public target is forbidden: $target"
    fi
  done < <(awk '/^[a-zA-Z0-9_.-]+:.*## / {split($1, a, ":"); print a[1]}' "$makefile")
}

echo "[swobucom-make-policy] checking public target grammar"
check_public_targets "$SWOBUCOM_MAKEFILE" "swobucom/Makefile" "$root_allowed_pattern"
check_public_targets "$SITE_MAKEFILE" "swobucom/site/Makefile" "$surface_allowed_pattern"

echo "[swobucom-make-policy] checking root-to-surface delegation"
if rg -n 'cd "\$\(SITE_DIR\)".*scripts/' "$SWOBUCOM_MAKEFILE" >/dev/null 2>&1; then
  fail "swobucom/Makefile must delegate site internals via make -C site, not direct script calls"
fi

echo "[swobucom-make-policy] checking single visual test lane"
if ! rg -n '^test-e2e-visual-regression:.*## ' "$SITE_MAKEFILE" >/dev/null 2>&1; then
  fail "swobucom/site/Makefile must expose a canonical test-e2e-visual-regression lane"
fi

echo "[swobucom-make-policy] policy checks passed"
