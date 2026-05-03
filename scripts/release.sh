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
cd "$oss_root"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "release failed: required command not found: $1" >&2
    exit 1
  fi
}

sha256_of() {
  file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  echo "release failed: sha256 tool not found (need sha256sum or shasum)" >&2
  exit 1
}

need_cmd git
need_cmd go
need_cmd gh
need_cmd tar

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
make verify

echo "release: tagging $next_tag"
git tag -a "$next_tag" -m "release $next_tag"
git push origin "$next_tag"
echo "release: pushed $next_tag"

echo "release: building unix artifacts for $next_tag"
version="${next_tag#v}"
module_path="$(go list -m -f '{{.Path}}')"
ldflags="-s -w -X ${module_path}/internal/app/operator/controlplane.swobuVersion=${version}"
release_dist_dir="$oss_root/dist/release/$next_tag"
rm -rf "$release_dist_dir"
mkdir -p "$release_dist_dir"

for os in linux darwin; do
  for arch in amd64 arm64; do
    archive_name="swobu_${next_tag}_${os}_${arch}.tar.gz"
    stage_dir="$release_dist_dir/stage-${os}-${arch}"
    rm -rf "$stage_dir"
    mkdir -p "$stage_dir"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$stage_dir/swobu" ./cmd/swobu
    tar -C "$stage_dir" -czf "$release_dist_dir/$archive_name" swobu
    rm -rf "$stage_dir"
  done
done

checksums_file="$release_dist_dir/checksums.txt"
: >"$checksums_file"
for archive in "$release_dist_dir"/swobu_"$next_tag"_*.tar.gz; do
  archive_base="$(basename "$archive")"
  checksum="$(sha256_of "$archive")"
  printf '%s  %s\n' "$checksum" "$archive_base" >>"$checksums_file"
done

echo "release: creating GitHub Release $next_tag"
gh release create "$next_tag" "$release_dist_dir"/swobu_"$next_tag"_*.tar.gz "$checksums_file" \
  --repo swobuforge/swobu \
  --verify-tag \
  --latest \
  --notes-from-tag
echo "release: published GitHub Release $next_tag"
