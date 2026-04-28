# Live Matrix Fixtures

`scenario_cases.json` defines low-cost live compatibility probes across provider/protocol/transport/model/scenario axes.
`scenario_cases.smoke.json` is the default manual refresh lane for credentialed remote providers (OpenAI, OpenRouter, Anthropic).

Run live capture (default credentialed remote-provider smoke lane):

```bash
make evidence-refresh
```

Run broad live matrix explicitly:

```bash
make evidence-refresh-full
```

This command is explicit and manual by policy. Live evidence artifacts are not
updated automatically during commit/push hooks.

Prerequisites for full live mining:
- OpenAI credentials (`OPENAI_API_KEY` preferred, or `SWOBU_OPENAI_KEY_FILE` with fallback `.secrets/openai.key`)
- OpenRouter credentials (`OPENROUTER_API_KEY` preferred, or `SWOBU_OPENROUTER_KEY_FILE` with fallback `.secrets/openrouter.key`)
- Anthropic credentials (`ANTHROPIC_API_KEY` preferred, or `SWOBU_ANTHROPIC_KEY_FILE` with fallback `.secrets/anthropic.key`, `.secrets/claude.key`)
- `aider` available in `PATH`

If `aider` is missing, live mining fails fast by design.

Containerized live mining (aider preinstalled in image):

```bash
make container-evidence-refresh
```

Containerized broad matrix:

```bash
make container-evidence-refresh-full
```

Equivalent provider-only command:

```bash
go run ./internal/devtools/cmd/livematrix \
  -cases test/fixtures/live_matrix/scenario_cases.json \
  -out test/fixtures/live_matrix/records
```

Default minimal one-case lane:

```bash
go run ./internal/devtools/cmd/liveevidencemine \
  -cases test/fixtures/live_matrix/scenario_cases.smoke.json
```

Default capture mode is `swobu_session` (client -> Swobu -> provider, with
session transcript). Use `-mode direct` only for provider-edge archaeology.

Environment variables are read per scenario case via `*_env` fields (for example `OPENAI_API_KEY`, `OPENAI_MODEL`).

Each run writes one JSON capture per scenario case under `records/` with:
- resolved scenario case metadata
- outbound request (method/url/headers/body)
- live upstream response (status/headers/body)
- duration and failure details
- secrets are not persisted (`Authorization` headers are excluded)

Offline replay test consumes these captures:

```bash
go test ./test/integration/providers -run TestLiveMatrixReplay_RecordedCapturesDecodeOffline -count=1
```
