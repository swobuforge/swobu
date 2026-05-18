# Troubleshooting

## Quick checks

1. `swobu status`
2. Verify client points to Swobu local endpoint.
3. Verify backend auth and model selection in cockpit.
4. Confirm request family compatibility.

## Installer

The release installer currently supports Linux/macOS on `amd64` and `arm64`.

If release installer is unavailable, use source install:

```sh
go install github.com/swobuforge/swobu/cmd/swobu@master
```

## Compatibility reports

When filing an issue, include:
- client + backend pair
- request family
- expected vs actual behavior
- redacted logs/screenshots
