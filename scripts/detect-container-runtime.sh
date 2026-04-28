#!/usr/bin/env bash
set -euo pipefail

requested="${CONTAINER_RUNTIME:-auto}"
if [[ "$requested" != "auto" ]]; then
  echo "$requested"
  exit 0
fi

if command -v podman >/dev/null 2>&1; then
  echo "podman"
  exit 0
fi
if command -v docker >/dev/null 2>&1; then
  echo "docker"
  exit 0
fi

echo "no supported container runtime found (podman/docker)" >&2
exit 1

