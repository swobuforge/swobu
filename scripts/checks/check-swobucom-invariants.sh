#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../../.." && pwd)"
SITE_DIR="$ROOT_DIR/swobucom/site"

fail() {
  echo "invariant failed: $1" >&2
  exit 1
}

echo "[swobucom-invariants] checking local-font invariant"
if rg -n "fonts.googleapis|fonts.gstatic" "$SITE_DIR/index.html" "$SITE_DIR/styles.css" >/dev/null 2>&1; then
  fail "site must not depend on remote Google Fonts runtime imports"
fi

echo "[swobucom-invariants] checking semantic path naming policy"
if find "$ROOT_DIR/swobucom/site" "$ROOT_DIR/swobucom/api" \
  \( -type d \( -name node_modules -o -name .git -o -name .codex -o -name .agents \) -prune \) \
  -o -mindepth 1 -print \
  | sed "s#^$ROOT_DIR/##" \
  | grep -E '(^|[^[:alnum:]])(legacy|deprecated|temporary|temp)([^[:alnum:]]|$)' >/dev/null; then
  fail "forbidden transitional lexeme found in swobucom semantic paths"
fi

echo "[swobucom-invariants] checking make target policy"
"$ROOT_DIR/swobucli/scripts/checks/check-swobucom-make-policy.sh"

echo "[swobucom-invariants] checking site script stack invariant"
[[ -f "$SITE_DIR/scripts/capture.ts" ]] || fail "missing site/scripts/capture.ts"
[[ -f "$SITE_DIR/scripts/visual-diff.ts" ]] || fail "missing site/scripts/visual-diff.ts"
[[ -f "$SITE_DIR/scripts/check-ui-css-grammar.ts" ]] || fail "missing site/scripts/check-ui-css-grammar.ts"
[[ -f "$SITE_DIR/eslint.config.mjs" ]] || fail "missing site/eslint.config.mjs"
[[ -f "$SITE_DIR/.stylelintrc.cjs" ]] || fail "missing site/.stylelintrc.cjs"
[[ -f "$SITE_DIR/.htmlvalidate.json" ]] || fail "missing site/.htmlvalidate.json"
[[ -d "$SITE_DIR/test/baselines/full-page" ]] || fail "missing site/test/baselines/full-page"
[[ -d "$SITE_DIR/test/baselines/first-screen" ]] || fail "missing site/test/baselines/first-screen"
[[ -d "$SITE_DIR/test/artifacts/visual" ]] || fail "missing site/test/artifacts/visual"
if [[ -d "$SITE_DIR/design" ]]; then
  fail "site/design is deprecated; use site/test/* ontology for visual tests"
fi
if compgen -G "$SITE_DIR/scripts/*.mjs" >/dev/null; then
  fail "site scripts must be TypeScript-first; found .mjs script(s)"
fi

node <<'NODE'
const fs = require('fs');
const path = require('path');

const root = process.cwd();
const sitePkgPath = path.join(root, 'swobucom', 'site', 'package.json');

function fail(message) {
  process.stderr.write(`invariant failed: ${message}\n`);
  process.exit(1);
}

function expectScript(pkg, name, pkgName) {
  if (!pkg.scripts || typeof pkg.scripts[name] !== 'string' || pkg.scripts[name].trim() === '') {
    fail(`${pkgName} missing required script: ${name}`);
  }
}

function expectDep(pkg, name, pkgName) {
  if (!pkg.devDependencies || typeof pkg.devDependencies[name] !== 'string') {
    fail(`${pkgName} missing required devDependency: ${name}`);
  }
}

const sitePkg = JSON.parse(fs.readFileSync(sitePkgPath, 'utf8'));
expectScript(sitePkg, 'screenshot', 'swobucom/site');
expectScript(sitePkg, 'visual-diff', 'swobucom/site');
expectScript(sitePkg, 'typecheck', 'swobucom/site');
expectScript(sitePkg, 'lint', 'swobucom/site');
expectScript(sitePkg, 'lint:ui-grammar', 'swobucom/site');
expectScript(sitePkg, 'lint:ts', 'swobucom/site');
expectScript(sitePkg, 'lint:css', 'swobucom/site');
expectScript(sitePkg, 'lint:html', 'swobucom/site');
expectDep(sitePkg, 'typescript', 'swobucom/site');
expectDep(sitePkg, 'tsx', 'swobucom/site');
expectDep(sitePkg, 'stylelint', 'swobucom/site');
expectDep(sitePkg, 'playwright', 'swobucom/site');

process.stdout.write('[swobucom-invariants] package invariants ok\n');
NODE

echo "[swobucom-invariants] all invariants passed"
