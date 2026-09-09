# Troubleshooting

## Quick checks

1. `swobu status`
2. Verify client points to Swobu local endpoint.
3. Verify backend auth and model selection in cockpit.

## Installer

Release packages cover Linux, macOS, and Windows on `amd64` and `arm64`.
See the [installation instructions](../README.md#start-routing-in-one-command) for your platform.

To build from source, install the Go version specified in `go.mod` and Make:

```sh
git clone https://github.com/swobuforge/swobu.git
cd swobu
make build
./.out/swobu --version
```

## Issue reports

When filing an issue, include:
- client + backend pair
- expected vs actual behavior
- redacted logs/screenshots
