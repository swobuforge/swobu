#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

if [ "$#" -ne 2 ]; then
	echo "usage: swobucli/scripts/create-task-from-template.sh task-group short-change-title" >&2
	exit 1
fi

group="$1"
slug=$(printf '%s' "$2" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g; s/-\{2,\}/-/g; s/^-//; s/-$//')

if [ -z "$slug" ]; then
	echo "task title produced empty slug" >&2
	exit 1
fi

dir="$ROOT_DIR/tasks/ready/$group"
template="$ROOT_DIR/tasks/templates/task-frame-template.md"
target="$dir/$slug.md"

if [ ! -f "$template" ]; then
	echo "missing template: $template" >&2
	exit 1
fi

if [ ! -d "$dir" ]; then
	echo "missing task group: $dir" >&2
	echo "available groups:" >&2
	find "$ROOT_DIR/tasks/ready" -mindepth 1 -maxdepth 1 -type d -printf '  %f\n' | sort >&2
	exit 1
fi

if [ -e "$target" ]; then
	echo "task already exists: $target" >&2
	exit 1
fi

cp "$template" "$target"
printf 'created %s\n' "${target#$ROOT_DIR/}"
