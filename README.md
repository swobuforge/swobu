# [Swobu](https://swobu.com/)

**English** · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Português (Brasil)](README.pt-BR.md) · [Bahasa Indonesia](README.id.md) · [한국어](README.ko.md) · [Русский](README.ru.md) · [Español](README.es.md) · [Українська](README.uk.md)

**One endpoint for your AI agents. Any LLM capacity underneath.**

Make AI capacity routable. Your agent asks for a model. Swobu turns that model name into a route across providers, accounts, regions, and local servers — with balancing, failover, reasoning translation, and semantic protocol compatibility underneath.

[Documentation](https://swobu.com/docs/) · [Quickstart](https://swobu.com/docs/start/first-route/) · [VS Code extension](https://marketplace.visualstudio.com/items?itemName=swobu.swobu&utm_source=swobu_docs&utm_medium=referral&utm_campaign=vscode_extension) · [Releases](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Agents and clients supported by Swobu" width="900">
  <img src="./assets/readme/providers.png" alt="Providers supported by Swobu" width="1100">
</p>

---

## Your agent chooses a model. Swobu chooses where it runs.

A Swobu **route looks like a model** to your agent.

Behind that name can be one endpoint, the same model available from several places, or a cross-provider pool.

The diagrams illustrate configurations. Choose models available from your providers.

```text
claude-opus-5
    │
    ├─ Anthropic / claude-opus-5
    ├─ AWS Bedrock / account A / claude-opus-5
    └─ AWS Bedrock / account B / claude-opus-5
```

Keep using `claude-opus-5`. Swobu can balance capacity and fail over underneath it.

Or make the model name describe a job:

```text
codex-auto-review
    │
    ├─ Deepseek / Deepseek V4 Flash
    ├─ Google / Gemini 3.7 Flash
    └─ another review model
```

Or build a pool that deliberately crosses models and providers:

```text
free
    │
    ├─ Cerebras / Gemma 4 31B
    ├─ Groq / gpt-oss-20b
    ├─ LLM7 / default
    ├─ OpenRouter / free
    ├─ Mistral / Ministral 3B
    ├─ NVIDIA NIM / Nemotron Mini 4B
    └─ Ollama / Qwen 3.8 27b
```

The model field your agent already understands becomes a programmable routing boundary.

---

## Start routing in one command

macOS, Linux, or WSL:

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://swobu.com/install.ps1 | iex
```

The installer opens **Cockpit**, where you can add a provider, create a route,
and connect your first agent. It verifies the download, preserves an existing
standalone installation if setup fails, and leaves your shell profile and Swobu
data alone.

Already using a standalone installation? Update it with:

```text
swobu update
```

Source, package-manager, and custom-directory installations remain owned by
the method that installed them.

[Build your first route in five minutes →](https://swobu.com/docs/start/first-route/)

### Connect an agent

Cockpit can configure supported clients for you.

Or use the CLI:

```bash
swobu connect claude
swobu connect codex
swobu connect muse
swobu connect openclaw
swobu connect pi
swobu connect kilo
swobu connect opencode
swobu connect hermes
```

After that, your agent talks to Swobu. Provider configuration and routing stay behind the gateway.

---

## What changes when the model name becomes a route?

### Pool capacity

A target is not just a model.

It can represent a particular:

- provider
- account
- cloud region
- hosted endpoint
- local server
- model

Put several targets in the same tier to balance across them.

Add fallback tiers to define what happens when preferred capacity is unavailable.

```text
route: gpt-5.6-sol

primary
├─ Azure / westcentralus / gpt-5.6-sol
└─ Azure / westus2 / gpt-5.6-sol

fallback
└─ OpenAI / gpt-5.6-sol
```

The agent still asks for `gpt-5.6-sol`.

---

### Route across providers

Routes don't have to preserve model identity.

A name such as `review`, `cheap`, `free`, or `codex-auto-review` can represent whatever capacity makes sense for that workload.

```text
review
├─ Z.AI / GLM-5.3
├─ Kimi / Kimi-2.8
└─ Ollama / Qwen3-Coder
```

This lets different agents share routing policy without hard-coding provider configuration into each one.

---

### Fail over without reconfiguring the agent

Quota exhausted. Region unavailable. Endpoint fails. Account hits a limit.

Swobu can try the next eligible target according to the route.

```text
agent
  │
  │ model: gpt-5.6-sol
  ▼
Swobu
  │
  ├─ Azure ────── unavailable
  │
  └─ OpenAI ──────── ✓
```

The route name does not change.

---

## One boundary, multiple protocols

```text
Claude Code ─┐
Codex ───────┤
Muse Code ───┤
OpenClaw ────┤
Pi ──────────┤
Kilo ────────┤
OpenCode ────┼──── Swobu ────┬─ OpenAI
Hermes ──────┤                ├─ Anthropic
Other agents ┘                ├─ Gemini
                              ├─ AWS Bedrock
                              ├─ Azure AI
                              ├─ Cerebras
                              ├─ Cloudflare
                              ├─ Ollama
                              ├─ LM Studio
                              ├─ vLLM
                              └─ ...
```

Swobu currently supports provider integrations across protocols including:

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages
- Gemini Interactions

Exact protocol and capability support varies by provider.

[Capability matrix →](https://swobu.com/docs/)

---

## Providers

Swobu supports local inference, frontier APIs, hyperscalers, specialized inference platforms, and aggregators.

[Find providers and setup instructions in the documentation.](https://swobu.com/docs/)

---

## Examples

### Same model, multiple providers

Keep the model name the agent already uses while adding redundant capacity underneath it.

### Cross-provider free pool

Combine recurring free capacity behind one model name.

### Local first, cloud when needed

Prefer Ollama, LM Studio, or vLLM and fall through to hosted capacity according to policy.

### Agent-specific routes

Expose names such as `codex-auto-review` or `claude-plan` while changing the providers and models behind them independently.

---

## Local-first

Swobu runs locally and exposes the endpoint your agents connect to.

Your provider credentials stay at the gateway rather than being copied into every client.

No Swobu account is required for local use.

Operational telemetry is deliberately limited, and can be disabled.

[Security & privacy →](https://swobu.com/docs/)

---

## Releases

Swobu publishes versioned binaries for Linux, macOS, and Windows, with SHA-256 checksums.

[Latest release →](https://github.com/swobuforge/swobu/releases/latest)

Build from source:

```bash
git clone https://github.com/swobuforge/swobu.git
cd swobu
make build
./.out/swobu --version
```

---

<p align="center">
  <strong>One model name. Any capacity underneath.</strong>
</p>

<p align="center">
  <a href="https://swobu.com/docs/start/first-route/">Get started</a>
  ·
  <a href="https://swobu.com/docs/">Docs</a>
  ·
  <a href="https://github.com/swobuforge/swobu/releases">Releases</a>
</p>
