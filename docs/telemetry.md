# Telemetry

Swobu is local-first, not offline-only.

If you route to hosted backends, requests still go to those backends.

## Controls

```sh
swobu telemetry status
swobu telemetry on
swobu telemetry off
```

## What is not collected by default telemetry

- prompt content
- completion content
- auth material

## Why telemetry exists

Telemetry provides product/runtime usage signals for improving compatibility and reliability without routing model content through telemetry payloads.
