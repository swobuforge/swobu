# swobucli/scripts

Thin CLI project wrappers and bootstrap launchers.

Rules:
- wrappers stay thin
- durable policy logic lives in `internal/devtools/*`

Script index:
- `install.sh`: operator installer entrypoint for release binaries (download, checksum verify, install)
