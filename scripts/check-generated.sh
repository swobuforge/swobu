#!/usr/bin/env sh
set -eu

root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT HUP INT TERM
cp "$root/go.mod" "$root/go.sum" "$stage/"
mkdir -p "$stage/internal"
cp -R "$root/internal/cockpit" "$stage/internal/"
find "$stage/internal/cockpit" -name '*_gsx.go' -type f -delete
(cd "$stage" && GOWORK=off GOENV=off GOFLAGS= GOTOOLCHAIN=local go generate ./internal/cockpit)
diff -qr "$root/internal/cockpit" "$stage/internal/cockpit"
