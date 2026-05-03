#!/usr/bin/env sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/release.sh patch|minor|major" >&2
  exit 1
fi

kind="$1"
case "$kind" in
  patch|minor|major) ;;
  *)
    echo "invalid release bump kind: $kind (expected: patch|minor|major)" >&2
    exit 1
    ;;
esac

oss_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
repo_root="$(CDPATH= cd -- "$oss_root/../.." && pwd)"
cd "$repo_root"

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "release failed: working tree has tracked changes; commit/stash first" >&2
  exit 1
fi

latest_tag="$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1)"
if [ -z "$latest_tag" ]; then
  latest_tag="v0.0.0"
fi

version_core="$(printf '%s' "$latest_tag" | sed -E 's/^v([0-9]+\.[0-9]+\.[0-9]+)$/\1/')"
if [ "$version_core" = "$latest_tag" ]; then
  echo "release failed: latest semver tag is malformed: $latest_tag" >&2
  exit 1
fi

major="$(printf '%s' "$version_core" | cut -d. -f1)"
minor="$(printf '%s' "$version_core" | cut -d. -f2)"
patch="$(printf '%s' "$version_core" | cut -d. -f3)"

case "$kind" in
  patch)
    patch=$((patch + 1))
    ;;
  minor)
    minor=$((minor + 1))
    patch=0
    ;;
  major)
    major=$((major + 1))
    minor=0
    patch=0
    ;;
esac

next_tag="v${major}.${minor}.${patch}"
if git rev-parse "$next_tag" >/dev/null 2>&1; then
  echo "release failed: tag already exists: $next_tag" >&2
  exit 1
fi

echo "release: running verify"
make -C swobucli/oss verify

echo "release: tagging $next_tag"
git tag -a "$next_tag" -m "release $next_tag"
git push origin "$next_tag"
echo "release: pushed $next_tag"
