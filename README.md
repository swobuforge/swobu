# Swobu

![Swobu README hero](./assets/readme/swobu-readme-hero.png)

Swobu is a local AI compatibility layer that unbundles AI clients from LLM backends.

Use Claude Code, Codex CLI, Continue, or an OpenAI/Anthropic-compatible client. Point it at Swobu. Connect it to OpenAI, Anthropic, OpenRouter, Ollama, or a custom OpenAI-compatible backend.

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

## Quickstart

Install:

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Start interactive setup:

```bash
swobu
```

Swobu will guide you through configuring a local gateway and connecting your client to a backend.

---

## How It Works

Swobu runs locally and exposes familiar API surfaces to AI clients.

Your client talks to Swobu.
Swobu talks to the backend.
Swobu shims the incompatibilities between them.

```txt
AI Client  ->  Swobu  ->  LLM Backend
```

Example:

```txt
Claude Code  ->  Swobu  ->  OpenRouter
Codex CLI    ->  Swobu  ->  Ollama
Continue     ->  Swobu  ->  Anthropic
```

The goal is not to replace your AI client.

The goal is to stop your AI client from deciding your backend.

---

## Current Surface

Swobu is currently in beta.

### Protocol Families

Supported API surfaces:

- OpenAI-style:
  - `/v1/chat/completions`
  - `/v1/responses`
  - `/v1/completions`
- Anthropic-style:
  - `/v1/messages`
- Streaming:
  - Server-Sent Events
  - WebSocket

### Supported Clients

Currently tested with:

- Claude Code
- Codex CLI
- Continue
- OpenAI-compatible clients
- Anthropic-compatible clients

Supported clients today, designed for more.

### Supported Backends

Currently supported:

- OpenAI
- Anthropic
- OpenRouter
- Ollama
- Custom OpenAI-compatible backends

---

## Client Configuration

Swobu is designed to work through client configuration, not through a required SDK.

You point your client at the local Swobu endpoint, then choose the backend Swobu should use.

Typical setup uses:

- base URL configuration
- environment variables
- client config files
- local backend profiles

Example shape:

```bash
OPENAI_BASE_URL=http://localhost:PORT/v1
OPENAI_API_KEY=swobu
```

Exact configuration depends on the client.

Swobu's job is to make those client/backend combinations easier to wire together and easier to change later.

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

Swobu is local-first.

By default, Swobu:

- binds to loopback
- keeps control on your machine
- avoids sending prompts, completions, and auth material through default telemetry

Telemetry defaults to aggregate operational signals only and can be turned off.

Check telemetry status:

```bash
swobu telemetry status
```

Turn telemetry off:

```bash
swobu telemetry off
```

Do not confuse local-first with "offline-only."

If you configure Swobu to use a hosted backend, requests are still sent to that backend.

Swobu gives you a local boundary. It does not magically make remote providers local.

---

## Example Use Cases

### Use a preferred client with a different backend

Keep the client experience you like while changing the model provider underneath.

```txt
Client stays the same.
Backend changes.
Workflow survives.
```

### Test local and hosted models behind one client

Move between Ollama, OpenAI, Anthropic, OpenRouter, or compatible backends without rewriting client glue.

### Reduce workflow lock-in

Your AI client should be replaceable.
Your backend should be replaceable.
Your workflow should not be hostage to either.

### Normalize incompatibilities

Different clients and backends disagree on request shape, streaming behavior, authentication, and error handling.

Swobu absorbs those differences at the local edge.

---

## Roadmap Direction

Swobu's near-term focus is interoperability.

Priorities include:

- more client profiles
- more backend profiles
- better config generation
- better compatibility diagnostics
- clearer error translation
- stronger streaming support
- safer local defaults
- easier backend hot-swapping

The goal is simple:

Make it boring to connect supported AI clients to the backend you choose.

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

See [`LICENSE`](./LICENSE).
