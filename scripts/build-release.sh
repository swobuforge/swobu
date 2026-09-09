#!/usr/bin/env sh
set -eu

if [ "$#" -ne 4 ]; then
  echo "usage: build-release.sh VERSION SOURCE_COMMIT SOURCE_TREE OUTPUT_DIRECTORY" >&2
  exit 2
fi

version=$1
source_commit=$2
source_tree=$3
output=$4
root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
export GOWORK=off GOENV=off GOFLAGS= GOTOOLCHAIN=local CGO_ENABLED=0 GOFIPS140=off TZ=UTC LC_ALL=C
unset GOOS GOARCH GOAMD64 GOARM64 GOEXPERIMENT

case "$version" in
  *[!0-9A-Za-z.-]*) echo "invalid product version" >&2; exit 2 ;;
  v*) ;;
  *) echo "product version must start with v" >&2; exit 2 ;;
esac
printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$' || {
  echo "invalid product version" >&2
  exit 2
}
for object in "$source_commit" "$source_tree"; do
  case "$object" in
    *[!0-9a-f]*|'')
      echo "source identities must be full Git object IDs" >&2
      exit 2
      ;;
  esac
  test "${#object}" -eq 40 || test "${#object}" -eq 64 || {
    echo "source identities must be full Git object IDs" >&2
    exit 2
  }
done

required_go="$(awk '$1 == "go" { print "go" $2 }' "$root/go.mod")"
if [ "$(go env GOVERSION)" != "$required_go" ]; then
  echo "release build requires $required_go" >&2
  exit 1
fi
if [ -e "$output" ]; then
  echo "output directory must not exist" >&2
  exit 1
fi
parent="$(CDPATH= cd -- "$(dirname -- "$output")" && pwd)"
output="$parent/$(basename -- "$output")"
stage="$(mktemp -d "$parent/.swobu-release-XXXXXXXX")"
build="$(mktemp -d)"
trap 'rm -rf "$stage" "$build"' EXIT HUP INT TERM

go -C "$root" build -mod=readonly -trimpath -buildvcs=false -o "$build/package-release" ./scripts/package-release
for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64 windows-arm64; do
  os=${target%-*}
  arch=${target#*-}
  binary=swobu
  format=tar.gz
  if [ "$os" = windows ]; then
    binary=swobu.exe
    format=zip
  fi
  mkdir "$build/$target"
  GOOS="$os" GOARCH="$arch" go -C "$root" build -mod=readonly -trimpath -buildvcs=false \
    -ldflags "-s -w -X github.com/swobuforge/swobu/internal/app/operator/controlplane.swobuVersion=$version" \
    -o "$build/$target/$binary" ./cmd/swobu
  "$build/package-release" "$stage/swobu_${version}_${os}_${arch}.$format" "$build/$target/$binary"
done
cp "$root/scripts/install.sh" "$stage/install.sh"
cp "$root/scripts/install.ps1" "$stage/install.ps1"
printf '{"version":"%s","source_commit":"%s","source_tree":"%s","go":"%s"}\n' \
  "$version" "$source_commit" "$source_tree" "$required_go" > "$stage/build-manifest.json"
(
  cd "$stage"
  for asset in build-manifest.json install.ps1 install.sh swobu_*; do
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$asset"
    else
      shasum -a 256 "$asset"
    fi
  done > checksums.txt
)
mv "$stage" "$output"
