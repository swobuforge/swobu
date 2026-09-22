#!/usr/bin/env sh
set -eu

root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
carriage_return="$(printf '\r')"

find "$root" -type f -name '*.ansi' -print | while IFS= read -r fixture; do
  if LC_ALL=C grep "$carriage_return" "$fixture" >/dev/null; then
    echo "ANSI fixture contains a carriage return: $fixture" >&2
    exit 1
  fi
  attribute="$(git -C "$root" check-attr eol -- "$fixture")"
  case "$attribute" in
    *': eol: lf') ;;
    *) echo "ANSI fixture is not governed by eol=lf: $fixture ($attribute)" >&2; exit 1 ;;
  esac
done
