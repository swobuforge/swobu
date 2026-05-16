# Swobu

![Swobu README hero](./assets/readme/swobu-readme-hero.png)

Swobu is a local AI compatibility layer that unbundles AI clients from LLM backends.

Use Claude Code, Codex CLI, Continue, or another OpenAI/Anthropic-compatible client. Point it at Swobu. Connect Swobu to OpenAI, Anthropic, OpenRouter, Ollama, or a custom OpenAI-compatible backend.

The client is not the brain.

---

## Why Swobu

AI clients often come with backend assumptions baked in.

That coupling gets painful when you want to:

- switch providers
- compare models
- run local inference
- use a cheaper backend
- keep your existing workflow
- avoid rewriting client-specific glue
- keep one local control boundary for AI traffic

Swobu sits between the client and the backend and absorbs the mismatch.

It handles differences in:

- protocol shape
- streaming semantics
- authentication assumptions
- error models
- client/backend quirks

Keep the client. Swap the backend. Control the boundary.

---

## Demo

![Swobu first demo](./assets/readme/swobu-cli-demo.gif)

---

## Quickstart (Lazy Path: Just Make It Work)

Install:

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Launch cockpit (interactive operator mode):

```bash
swobu
```

In cockpit:

1. Create or select a workspace.
2. Choose provider (`OpenAI`, `Anthropic`, `OpenRouter`, `Ollama`, or `custom`).
3. Fill required routing/auth fields.
4. Save.
5. Use the shown local client URL/route for your client.

Verify daemon health in another terminal:

```bash
swobu status
```

Expected: JSON output and exit code `0` (healthy) or `1` (running but uninitialized/degraded).

If the daemon is not reachable, you get exit code `2`.

---

## Quickstart (Scripted / Non-Interactive)

Start foreground daemon:

```bash
swobu daemon
```

Optional explicit config path:

```bash
swobu daemon --config /path/to/swobu.yaml
```

Check status from scripts/CI:

```bash
swobu status
```

Shutdown gracefully:

```bash
swobu down
```

---

## Install/Run From Source (Optional)

If you want to run the latest `master` directly:

```bash
go run github.com/swobuforge/swobu/cmd/swobu@master --help
```

If you want a local binary from `master` in your `GOBIN`:

```bash
go install github.com/swobuforge/swobu/cmd/swobu@master
swobu --help
```

Use this path when you explicitly want latest source behavior rather than the
stable install script channel.

---

## Command Surface (v0)

Swobu keeps a narrow operator command surface:

- `swobu` (interactive cockpit launcher)
- `swobu daemon`
- `swobu status`
- `swobu down`
- `swobu telemetry status|on|off`
- `swobu version`

Discover command help and defaults:

```bash
swobu --help
swobu status --help
swobu daemon --help
swobu telemetry status --help
```

---

## What You Can Do In Cockpit (TUI)

Cockpit is the operator UI over real daemon/domain truth. Use it to:

- create, rename, and delete workspaces/endpoints
- choose provider and route target per workspace
- configure provider settings (including base URL/auth references)
- inspect health/readiness state
- inspect traffic truth (request counts, success/error outcomes, timing classes)
- open help/feedback actions

Cockpit is not a fake demo shell. It is the primary interactive control surface for local setup and operations.

---

## Current Protocol Surface

Swobu is currently in beta.

Supported request families:

- OpenAI-style:
  - `/v1/chat/completions`
  - `/v1/responses`
  - `/v1/completions`
- Anthropic-style:
  - `/v1/messages`

Streaming support:

- Server-Sent Events
- WebSocket

---

## Supported Clients And Backends

### Clients (tested)

- Claude Code
- Codex CLI
- Continue
- OpenAI-compatible clients
- Anthropic-compatible clients

### Backends

- OpenAI
- Anthropic
- OpenRouter
- Ollama
- Custom OpenAI-compatible backends

---

## Scenario Playbooks (JBTD)

### 1) "I want to keep my client, change backend"

- Launch `swobu`.
- Keep your current client.
- Configure the backend in cockpit.
- Point client base URL to Swobu local endpoint.
- Verify with `swobu status`.

### 2) "I want local and hosted models behind one workflow"

- Start with one workspace (for example, local Ollama).
- Duplicate/switch workspace routing to hosted backend.
- Keep the same client UX while changing backend selection.

### 3) "I want scriptable ops, not UI"

- Run `swobu daemon` under your supervisor.
- Use `swobu status` for machine health checks.
- Use `swobu down` for controlled stop.

### 4) "I do not want to learn a new SDK"

- Do not adopt an SDK.
- Keep client-native configuration (base URL + key style your client already supports).
- Let Swobu normalize protocol/backend mismatch locally.

---

## Client Configuration Notes

Swobu is designed for client configuration, not SDK lock-in.

Typical shape:

```bash
OPENAI_BASE_URL=http://127.0.0.1:7926/v1
OPENAI_API_KEY=swobu
```

Notes:

- exact variable names depend on your client
- daemon URL default is `http://127.0.0.1:7926`
- cockpit-generated routing decides which backend receives traffic

If your client needs Anthropic-style endpoint shape, route it through the corresponding Swobu-supported surface.

---

## Telemetry Controls

Check telemetry state:

```bash
swobu telemetry status
```

Enable telemetry:

```bash
swobu telemetry on
```

Disable telemetry:

```bash
swobu telemetry off
```

Swobu is local-first by default, but remote traffic still goes to whichever backend you configure.

---

## Troubleshooting (Fast)

### `swobu status` returns `{"state":"down"}` / exit code `2`

- start daemon: `swobu` or `swobu daemon`
- verify daemon URL/port (`swobu status --help`)

### daemon start fails with address already in use

- stop existing daemon: `swobu down`
- rerun `swobu daemon`

### cockpit opens but endpoint is uninitialized

- finish workspace/provider configuration
- save and re-check with `swobu status`

### provider/auth errors during requests

- verify provider credentials and base URL in cockpit routing config
- verify selected workspace/provider mapping

---

## What Swobu Is

Swobu is:

- a local compatibility layer
- a protocol shim
- a client/backend boundary
- a way to hot-swap LLM backends behind existing AI clients

## What Swobu Is Not

Swobu is not currently:

- an SDK
- a hosted model marketplace
- a new AI client
- an observability platform
- a prompt management system

---

## Security and Privacy

By default, Swobu:

- binds to loopback
- keeps control on your machine
- avoids sending prompts, completions, and auth material through default telemetry

Telemetry defaults to aggregate operational signals and can be turned off.

Do not confuse local-first with offline-only.

If you configure a hosted backend, requests still go to that backend.

---

## Roadmap Direction

Near-term focus is interoperability depth:

- more client profiles
- more backend profiles
- better config generation
- better compatibility diagnostics
- clearer error translation
- stronger streaming support
- safer local defaults
- easier backend hot-swapping

Goal: make it boring to connect supported AI clients to the backend you choose.

---

## Contributing

Contributions are welcome.

Swobu uses a Contributor License Agreement.

By submitting a pull request or other contribution, you agree to the terms in [`CLA.md`](./CLA.md). This allows Swobu to maintain, sublicense, dual-license, and relicense contributions in the future.

Read [`CONTRIBUTING.md`](./CONTRIBUTING.md) before opening a pull request.

---

## Security

Do not report security vulnerabilities in public issues.

See [`SECURITY.md`](./SECURITY.md).

---

## License

Swobu is released under AGPL-3.0-only. Commercial licensing and additional
permissions are available by contacting contact@swobu.com.

See [`LICENSE`](./LICENSE).
