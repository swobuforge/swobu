#!/usr/bin/env sh
set -eu

root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
carriage_return="$(printf '\r')"
attributes="$root/.gitattributes"

if [ ! -f "$attributes" ] || ! awk '
  $1 == "*.ansi" && $2 == "text" && $3 == "eol=lf" { found = 1 }
  END { exit !found }
' "$attributes"; then
  echo "ANSI fixtures are not governed by *.ansi text eol=lf in $attributes" >&2
  exit 1
fi

find "$root" -type f -name '*.ansi' -print | while IFS= read -r fixture; do
  if LC_ALL=C grep "$carriage_return" "$fixture" >/dev/null; then
    echo "ANSI fixture contains a carriage return: $fixture" >&2
    exit 1
  fi
done
