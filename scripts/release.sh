#!/usr/bin/env sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/release.sh <version|vX.Y.Z>" >&2
  exit 1
fi

raw_version="$1"
version="${raw_version#v}"
tag="v$version"

oss_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$oss_root"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "artifact build failed: required command not found: $1" >&2
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
  echo "artifact build failed: sha256 tool not found (need sha256sum or shasum)" >&2
  exit 1
}

need_cmd go
need_cmd tar

module_path="$(go list -m -f '{{.Path}}')"
ldflags="-s -w -X ${module_path}/internal/app/operator/controlplane.swobuVersion=${version}"
release_dist_dir="$oss_root/dist/release/$tag"
rm -rf "$release_dist_dir"
mkdir -p "$release_dist_dir"

for os in linux darwin; do
  for arch in amd64 arm64; do
    archive_name="swobu_${tag}_${os}_${arch}.tar.gz"
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
for archive in "$release_dist_dir"/swobu_"$tag"_*.tar.gz; do
  archive_base="$(basename "$archive")"
  checksum="$(sha256_of "$archive")"
  printf '%s  %s\n' "$checksum" "$archive_base" >>"$checksums_file"
done

echo "artifact build OK: $release_dist_dir"
