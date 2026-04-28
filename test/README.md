# Test Suites

This repo uses a simple, orthogonal test model for onboarding and daily work.

## Four suites (engineer-facing)

1. `unit` — correctness of local code units (`./internal/...` tests).
2. `integration` — composition across internal package seams (`./test/integration/...`).
3. `e2e` — real runtime and operator journeys (`./test/e2e/...`).
4. `compatibility` — aggregate compatibility umbrella:
   - contract semantics (`./test/compatibility/surface/...`)
   - external-runtime compatibility lanes (`./test/compatibility/runtime/...`)

These are orthogonal by purpose:
- `unit`/`integration`/`e2e` are **system-level fidelity** suites.
- `contract` and `conformance` are **compatibility claim** suites.
- `compatibility` is an aggregate alias over `contract + conformance`.

## Canonical commands

- `make setup`
- `make dev`
- `make verify`
- `make verify-concurrency`
- `make run`
- `make evidence-refresh`
- `make test-unit`
- `make test-contract`
- `make test-conformance`
- `make test-integration`
- `make test-required`
- `make test-e2e`
- `make test-compatibility` (aggregate alias: contract + conformance)
- `make test` (required deterministic test bundle)
- `make test-integration-providers` (deterministic provider integration checks used by hermetic gate)
- `make test-all` (everything)
- `make check-required` (non-mutating pre-commit checks used by hooks)
- `make evidence-refresh-full` (manual non-blocking broad live matrix refresh)
- `make artifact-refs` (alias entrypoint for artifact reference policy checks)
- `make container-verify` (run deterministic verification in podman/docker)
- `make container-evidence-refresh` (run live evidence refresh in podman/docker)
- `make container-evidence-refresh-full` (run broad live matrix refresh in podman/docker)
- `make container-evidence-refresh-default` (run default evidence lane in podman/docker)

`make evidence-refresh` prerequisites: OpenAI credentials (`OPENAI_API_KEY` preferred, or `SWOBU_OPENAI_KEY_FILE` with fallback `.secrets/openai.key`), OpenRouter credentials (`OPENROUTER_API_KEY` preferred, or `SWOBU_OPENROUTER_KEY_FILE` with fallback `.secrets/openrouter.key`), Anthropic credentials (`ANTHROPIC_API_KEY` preferred, or `SWOBU_ANTHROPIC_KEY_FILE` with fallback `.secrets/anthropic.key`), plus `aider` binary in `PATH` (fails fast if missing).

`make container-evidence-refresh` prerequisites: OpenRouter credentials (`OPENROUTER_API_KEY` preferred, or `SWOBU_OPENROUTER_KEY_FILE` with fallback `.secrets/openrouter.key`). OpenAI and Anthropic credentials are optional at container wrapper level but required if selected scenario cases include those provider probes. Live mining and replay lanes share the same container image source (`test/container/hermetic/Containerfile`).

`make verify` includes blocking hermetic client e2e lanes:
- `TestHermeticClientConfigReplay_Aider`: exercises Aider copy/run actions from cockpit and asserts captured request semantics against deterministic replay responses.

## Why `compatibility` includes both contract and conformance lanes

`contract` and `conformance` are not separate product categories. They are two evidence paths for one compatibility claim:
- `contract` proves stable behavior on Swobu-owned interfaces.
- `conformance` proves compatibility against declared external runtimes.

Onboarding rule: treat them as one suite for support claims.
