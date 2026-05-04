#!/usr/bin/env sh
set -eu

REPO_OWNER="${REPO_OWNER:-swobuforge}"
REPO_NAME="${REPO_NAME:-swobu}"
PROJECT_NAME="${PROJECT_NAME:-swobu}"
BIN_NAME="${BIN_NAME:-swobu}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-}"
DRY_RUN="${DRY_RUN:-false}"
EXPECTED_SHA256="${EXPECTED_SHA256:-}"

usage() {
  cat <<'EOF'
Install swobu from GitHub Releases.

Usage:
  install.sh [--version vX.Y.Z] [--bin-dir /path] [--checksum <sha256>] [--dry-run]

Environment overrides:
  REPO_OWNER, REPO_NAME, PROJECT_NAME, BIN_NAME, INSTALL_DIR, VERSION, DRY_RUN, EXPECTED_SHA256
EOF
}

have_cmd() {
  command -v "$1" >/dev/null 2>&1
}

need_cmd() {
  if ! have_cmd "$1"; then
    echo "required command not found: $1" >&2
    exit 1
  fi
}

detect_os() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux|darwin) printf "%s" "$os" ;;
    *)
      echo "unsupported OS: $os (supported: linux, darwin)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) printf "amd64" ;;
    arm64|aarch64) printf "arm64" ;;
    *)
      echo "unsupported architecture: $arch (supported: amd64, arm64)" >&2
      exit 1
      ;;
  esac
}

http_get() {
  url="$1"
  out="$2"
  need_cmd curl
  curl -fsSL "$url" -o "$out"
}

resolve_version() {
  if [ -n "$VERSION" ]; then
    printf "%s" "$VERSION"
    return
  fi
  latest_url="https://api.github.com/repos/$REPO_OWNER/$REPO_NAME/releases/latest"
  latest_json="$tmp_root/latest.json"
  http_get "$latest_url" "$latest_json"
  tag="$(sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$latest_json" | head -n 1)"
  if [ -z "$tag" ]; then
    echo "failed to resolve latest release tag from $latest_url" >&2
    exit 1
  fi
  printf "%s" "$tag"
}

sha256_of() {
  file="$1"
  if have_cmd sha256sum; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if have_cmd shasum; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  echo "sha256 tool not found (need sha256sum or shasum)" >&2
  exit 1
}

normalize_hex256() {
  value="$(printf "%s" "$1" | tr '[:upper:]' '[:lower:]')"
  case "$value" in
    [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) printf "%s" "$value" ;;
    *)
      echo "invalid sha256 value: $1" >&2
      exit 1
      ;;
  esac
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      shift
      [ "$#" -gt 0 ] || { echo "--version requires a value" >&2; exit 1; }
      VERSION="$1"
      ;;
    --bin-dir)
      shift
      [ "$#" -gt 0 ] || { echo "--bin-dir requires a value" >&2; exit 1; }
      INSTALL_DIR="$1"
      ;;
    --dry-run)
      DRY_RUN=true
      ;;
    --checksum)
      shift
      [ "$#" -gt 0 ] || { echo "--checksum requires a value" >&2; exit 1; }
      EXPECTED_SHA256="$1"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
  shift
done

os="$(detect_os)"
arch="$(detect_arch)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT INT TERM

tag="$(resolve_version)"
archive="${PROJECT_NAME}_${tag}_${os}_${arch}.tar.gz"
base_url="https://github.com/$REPO_OWNER/$REPO_NAME/releases/download/$tag"
archive_url="$base_url/$archive"
checksums_url="$base_url/checksums.txt"

if [ "$DRY_RUN" = "true" ]; then
  echo "tag=$tag"
  echo "os=$os"
  echo "arch=$arch"
  echo "archive=$archive"
  echo "archive_url=$archive_url"
  echo "checksums_url=$checksums_url"
  echo "install_dir=$INSTALL_DIR"
  if [ -n "$EXPECTED_SHA256" ]; then
    echo "expected_sha256=$(normalize_hex256 "$EXPECTED_SHA256")"
  fi
  exit 0
fi

need_cmd tar
mkdir -p "$INSTALL_DIR"

archive_path="$tmp_root/$archive"
checksums_path="$tmp_root/checksums.txt"

echo "downloading: $archive_url"
http_get "$archive_url" "$archive_path"
echo "downloading: $checksums_url"
http_get "$checksums_url" "$checksums_path"

echo "Verifying artifact checksum"
expected="$(awk -v name="$archive" '
  NF >= 2 {
    f = $2
    sub(/^\*/, "", f)
    if (f == name) {
      print tolower($1)
      exit
    }
  }
' "$checksums_path")"
if [ -z "$expected" ]; then
  echo "archive $archive not found in checksums.txt" >&2
  exit 1
fi
actual="$(sha256_of "$archive_path")"
expected="$(normalize_hex256 "$expected")"
actual="$(normalize_hex256 "$actual")"
if [ "$expected" != "$actual" ]; then
  echo "error: checksum mismatch for $archive" >&2
  exit 1
fi
if [ -n "$EXPECTED_SHA256" ]; then
  pinned="$(normalize_hex256 "$EXPECTED_SHA256")"
  if [ "$pinned" != "$actual" ]; then
    echo "pinned checksum mismatch for $archive" >&2
    exit 1
  fi
else
  echo "warning: no pinned checksum provided; integrity checked via release checksums only" >&2
fi

extract_dir="$tmp_root/extract"
mkdir -p "$extract_dir"
if ! tar -tzf "$archive_path" | grep -qx "$BIN_NAME"; then
  echo "archive missing binary entry: $BIN_NAME" >&2
  exit 1
fi
tar -xzf "$archive_path" -C "$extract_dir" -- "$BIN_NAME"

if [ ! -f "$extract_dir/$BIN_NAME" ]; then
  echo "archive missing binary: $BIN_NAME" >&2
  exit 1
fi
if [ -L "$extract_dir/$BIN_NAME" ]; then
  echo "refusing symlink binary payload: $BIN_NAME" >&2
  exit 1
fi

install_path="$INSTALL_DIR/$BIN_NAME"
tmp_install="$INSTALL_DIR/.${BIN_NAME}.tmp.$$"
echo "Installing to $install_path"
cp "$extract_dir/$BIN_NAME" "$tmp_install"
chmod 0755 "$tmp_install"
mv -f "$tmp_install" "$install_path"
echo "$BIN_NAME installed successfully"
echo
echo "Run:"
echo "  $install_path --version"

path_case=":$PATH:"
case "$path_case" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo
    echo "Note: $INSTALL_DIR is not on your PATH."
    echo "Add it to your shell profile before running $BIN_NAME from a new terminal."
    ;;
esac
