# Search-presence RFC qualification receipt

Date: 2026-10-11
Base: `d5febc3189ca3e4283b6827c94767f85a219d669`

## Automated proof

- Focused Responses, canonical, client-connect, protocolcodec, OpenAI, and Gemini tests: pass.
- All Go packages except `internal/sharestate`: pass.
- `make check-build`, `make check-generated`, `make check-installer`, `make check-fixture-line-endings`, `make fmt-check`, and `go vet ./...`: pass.
- The complete `go test ./...` run has one unrelated stale fixture failure in `internal/sharestate`: `TestCertificateValidityRenewalAndRetryAreIndependent` uses a certificate that expired 2026-09-27. No sharestate code was changed.

## Client inventory

| Client | Installed build | Evidence status |
| --- | --- | --- |
| Codex | 0.160.1 | Wire-level cached/live preservation and qualified Responses lowering proven; real provider invocation not re-run in this qualification. |
| Claude Code | 2.1.274 | Installed; no new Swobu-specific trace captured. |
| Kilo Code | 7.5.6 | Connector config opt-in and reviewed-false replacement tests pass. |
| OpenCode | 1.18.11 | V1/V2 schema, precedence, conflict, preservation, and convergence tests pass. |
| pi | 0.85.0 | Installed; optional extension behavior not changed or fabricated. |
| Muse Code | 1.0.1 | Installed; no speculative connector change. |
| OpenClaw | 2026.7.1-2 | Installed; managed search configuration remains operator-owned. |
| Hermes Agent | unavailable | No evidence claimed. |
| Antigravity CLI | unavailable | No evidence claimed. |

The unavailable clients and the installed clients without a fresh end-to-end
trace are intentionally not marked green. This receipt records qualification
evidence, not a claim of universal backend search success.
