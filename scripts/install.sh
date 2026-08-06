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
VERBOSE="${VERBOSE:-false}"
START_SWOBU="${START_SWOBU:-true}"

say() { printf '%s\n' "$*" >&2; }
step() { say "→ $*"; }
ok() { say "✓ $*"; }
warn() { say "warning: $*"; }
die() {
  say "error: $*"
  exit 1
}
debug() {
  if [ "$VERBOSE" = "true" ]; then
    say "debug: $*"
  fi
}

usage() {
  cat <<EOF
Install Swobu.

Usage:
  install.sh [options]

Options:
  --version <tag>       Install a specific version, e.g. v0.3.1
  --bin-dir <path>      Install directory. Default: $HOME/.local/bin
  --checksum <sha256>   Require an exact SHA-256 checksum
  --dry-run             Show what would happen without installing
  --no-start            Install without starting Swobu
  --verbose             Show debug output
  -h, --help            Show help

Environment overrides:
  REPO_OWNER, REPO_NAME, PROJECT_NAME, BIN_NAME, INSTALL_DIR, VERSION, DRY_RUN, EXPECTED_SHA256, VERBOSE, START_SWOBU
EOF
}

have_cmd() {
  command -v "$1" >/dev/null 2>&1
}

need_cmd() {
  if ! have_cmd "$1"; then
    case "$1" in
      curl)
        die "curl is required.

Install it:
  macOS: brew install curl
  Ubuntu/Debian: sudo apt-get install curl
  Fedora: sudo dnf install curl"
        ;;
      tar)
        die "tar is required to unpack the Swobu archive."
        ;;
      *)
        die "required command not found: $1"
        ;;
    esac
  fi
}

detect_os() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux|darwin) printf "%s" "$os" ;;
    *)
      die "unsupported OS: $os (supported: linux, darwin)"
      ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) printf "amd64" ;;
    arm64|aarch64) printf "arm64" ;;
    *)
      die "unsupported architecture: $arch (supported: amd64, arm64)"
      ;;
  esac
}

http_get() {
  url="$1"
  out="$2"
  need_cmd curl
  curl_common_flags="-fL --retry 3 --retry-delay 1 --connect-timeout 15"
  token="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
  download_release_asset_via_api() {
    rel_url="$1"
    rel_out="$2"
    owner="$(printf '%s' "$rel_url" | sed -n 's#https://github.com/\([^/]*\)/\([^/]*\)/releases/download/.*#\1#p')"
    repo="$(printf '%s' "$rel_url" | sed -n 's#https://github.com/\([^/]*\)/\([^/]*\)/releases/download/.*#\2#p')"
    tag="$(printf '%s' "$rel_url" | sed -n 's#https://github.com/[^/]*/[^/]*/releases/download/\([^/]*\)/.*#\1#p')"
    asset_name="$(printf '%s' "$rel_url" | sed -n 's#.*/releases/download/[^/]*/\([^?]*\).*#\1#p')"
    if [ -z "$owner" ] || [ -z "$repo" ] || [ -z "$tag" ] || [ -z "$asset_name" ]; then
      return 1
    fi
    release_api="https://api.github.com/repos/$owner/$repo/releases/tags/$tag"
    release_json="$(curl -sS $curl_common_flags -H "Authorization: Bearer $token" -H "Accept: application/vnd.github+json" "$release_api")" || return 1
    asset_id="$(printf '%s' "$release_json" | awk -v name="$asset_name" '
      BEGIN { RS="{"; FS="," }
      index($0, "\"name\":\"" name "\"") || index($0, "\"name\": \"" name "\"") {
        for (i = 1; i <= NF; i++) {
          if ($i ~ /"id":[0-9]+/) {
            id = $i
            gsub(/[^0-9]/, "", id)
            print id
            exit
          }
        }
      }
    ')"
    if [ -z "$asset_id" ]; then
      return 1
    fi
    asset_api="https://api.github.com/repos/$owner/$repo/releases/assets/$asset_id"
    curl -fsSL -H "Authorization: Bearer $token" -H "Accept: application/octet-stream" "$asset_api" -o "$rel_out"
  }
  curl_cmd() {
    if [ -n "$token" ]; then
      curl -sS $curl_common_flags -H "Authorization: Bearer $token" "$url" -o "$out" || {
        case "$url" in
          https://github.com/*/releases/download/*)
            download_release_asset_via_api "$url" "$out"
            ;;
          *)
            return 1
            ;;
        esac
      }
    else
      if [ -t 2 ]; then
        curl -# $curl_common_flags "$url" -o "$out"
      else
        curl -sS $curl_common_flags "$url" -o "$out"
      fi
    fi
  }
  curl_cmd
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
    die "failed to resolve latest release tag from $latest_url"
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
  die "sha256 tool not found (need sha256sum or shasum)"
}

normalize_hex256() {
  value="$(printf "%s" "$1" | tr '[:upper:]' '[:lower:]')"
  if printf '%s' "$value" | grep -Eq '^[0-9a-f]{64}$'; then
    printf '%s' "$value"
    return
  fi
  die "invalid sha256 value: $1"
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
    --no-start)
      START_SWOBU=false
      ;;
    --verbose)
      VERBOSE=true
      ;;
    --checksum)
      shift
      [ "$#" -gt 0 ] || { die "--checksum requires a value"; }
      EXPECTED_SHA256="$1"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      say "unknown argument: $1"
      usage >&2
      exit 1
      ;;
  esac
  shift
done

os="$(detect_os)"
arch="$(detect_arch)"
tmp_root="$(mktemp -d)"
tmp_install=""
cleanup() {
  rm -rf "$tmp_root"
  if [ -n "${tmp_install:-}" ] && [ -f "$tmp_install" ]; then
    rm -f "$tmp_install"
  fi
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

tag="$(resolve_version)"
archive="${PROJECT_NAME}_${tag}_${os}_${arch}.tar.gz"
base_url="https://github.com/$REPO_OWNER/$REPO_NAME/releases/download/$tag"
archive_url="$base_url/$archive"
checksums_url="$base_url/checksums.txt"

if [ "$DRY_RUN" = "true" ]; then
  say "Swobu installer dry-run"
  echo "tag=$tag"
  echo "os=$os"
  echo "arch=$arch"
  echo "archive=$archive"
  echo "archive_url=$archive_url"
  echo "checksums_url=$checksums_url"
  echo "install_dir=$INSTALL_DIR"
  echo "start_swobu=$START_SWOBU"
  if [ -n "$EXPECTED_SHA256" ]; then
    echo "expected_sha256=$(normalize_hex256 "$EXPECTED_SHA256")"
  fi
  exit 0
fi

need_cmd tar
step "Detecting platform... $os $arch"
step "Resolving release... $tag"
step "Preparing install directory... $INSTALL_DIR"
mkdir -p "$INSTALL_DIR" || die "failed to create install directory: $INSTALL_DIR"

archive_path="$tmp_root/$archive"
checksums_path="$tmp_root/checksums.txt"

step "Downloading $archive"
http_get "$archive_url" "$archive_path"
step "Downloading checksums"
http_get "$checksums_url" "$checksums_path"

step "Verifying checksum"
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
  die "archive $archive not found in checksums.txt"
fi
actual="$(sha256_of "$archive_path")"
expected="$(normalize_hex256 "$expected")"
actual="$(normalize_hex256 "$actual")"
if [ "$expected" != "$actual" ]; then
  die "checksum mismatch for $archive"
fi
if [ -n "$EXPECTED_SHA256" ]; then
  pinned="$(normalize_hex256 "$EXPECTED_SHA256")"
  if [ "$pinned" != "$actual" ]; then
    die "pinned checksum mismatch for $archive"
  fi
fi

extract_dir="$tmp_root/extract"
mkdir -p "$extract_dir"
if ! tar -tzf "$archive_path" | grep -qx "$BIN_NAME"; then
  die "archive missing binary entry: $BIN_NAME"
fi
tar -xzf "$archive_path" -C "$extract_dir" -- "$BIN_NAME"

if [ ! -f "$extract_dir/$BIN_NAME" ]; then
  die "archive missing binary: $BIN_NAME"
fi
if [ -L "$extract_dir/$BIN_NAME" ]; then
  die "refusing symlink binary payload: $BIN_NAME"
fi

install_path="$INSTALL_DIR/$BIN_NAME"
if [ -d "$install_path" ]; then
  die "$install_path exists and is a directory"
fi
if [ ! -w "$INSTALL_DIR" ]; then
  die "install directory is not writable: $INSTALL_DIR

Try:
  install.sh --bin-dir /path/you/can/write

Or:
  sudo INSTALL_DIR=/usr/local/bin sh install.sh"
fi
if [ -x "$install_path" ]; then
  existing_version="$("$install_path" --version 2>/dev/null || true)"
  if [ -n "$existing_version" ]; then
    step "Found existing $BIN_NAME: $existing_version"
  else
    step "Found existing $BIN_NAME at $install_path"
  fi
fi
tmp_install="$INSTALL_DIR/.${BIN_NAME}.tmp.$$"
step "Installing to $install_path"
cp "$extract_dir/$BIN_NAME" "$tmp_install"
chmod 0755 "$tmp_install"
mv -f "$tmp_install" "$install_path"
step "Checking installation"
if "$install_path" --version >/dev/null 2>&1; then
  installation_verified=true
  ok "$BIN_NAME $tag installed"
else
  installation_verified=false
  warn "$BIN_NAME was installed, but '$BIN_NAME --version' failed."
  say "Try:"
  say "  $install_path --version"
fi

start_swobu() {
  if [ "$installation_verified" != "true" ]; then
    return
  fi
  if [ "$START_SWOBU" != "true" ]; then
    say ""
    say "Start Swobu:"
    say "  $install_path"
    return
  fi

  if ! exec 3<>/dev/tty; then
    say ""
    warn "Swobu was installed, but this session has no controlling terminal."
    say "Start it:"
    say "  $install_path"
    return
  fi

  say ""
  step "Starting Swobu"
  if ! "$install_path" <&3 >&3 2>&3; then
    warn "Swobu was installed, but startup failed."
    say "Try again:"
    say "  $install_path"
  fi
  exec 3>&- 3<&-
}

start_swobu

print_path_help() {
  case "${SHELL:-}" in
    */zsh) profile="$HOME/.zshrc" ;;
    */bash) profile="$HOME/.bashrc" ;;
    */fish)
      say ""
      warn "$INSTALL_DIR is not on your PATH."
      say "For fish, run:"
      say "  fish_add_path $INSTALL_DIR"
      return
      ;;
    *) profile="$HOME/.profile" ;;
  esac
  say ""
  warn "$INSTALL_DIR is not on your PATH."
  say "Add it:"
  say "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> $profile"
  say "  . $profile"
}

path_case=":${PATH:-}:"
case "$path_case" in
  *":$INSTALL_DIR:"*) ;;
  *) print_path_help ;;
esac
