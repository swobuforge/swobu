# Swobu
Local AI Gateway for connecting any AI client with any LLM backend through one controllable boundary.

_Pst. Hey, you. Join da stargazers, okay? Work work :)_

<a href="https://github.com/firecrawl/firecrawl">
  <img src="https://img.shields.io/github/stars/firecrawl/firecrawl.svg?style=social&label=Star&maxAge=2592000" alt="GitHub stars">
</a>


## 🧌 Why Swobu

Swapping clients and backends is where AI workflows usually break.
One client expects one protocol shape, one streaming behavior, one auth style, and one error model.
Another backend exposes different assumptions.

That interoperability gap forces teams to rewire configs, rewrite integration glue, or abandon the client/backend they want.

Swobu is the local boundary that absorbs that mismatch so you can:

- keep the client you already use
- connect it to supported backends without workflow migration
- keep endpoint behavior explicit and observable
- preserve a local-first trust model

## Quickstart

Install:

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Setup:

```bash
swobu 
```

## ⭐ What Works Today (Beta)

### Protocol surfaces

- OpenAI-style: `/v1/chat/completions`, `/v1/responses`, `/v1/completions`
- Anthropic-style: `/v1/messages`
- Streaming: SSE, WebSocket

### Supported clients

- Claude Code (`messages`)
- Codex CLI (`chat_completions`, `responses`, `completions`)
- Continue (`chat_completions`, `responses`, `completions`)
- OpenAI-compatible clients
- Anthropic-compatible clients

### Supported backends

- OpenAI
- OpenRouter
- Anthropic
- Ollama
- Custom OpenAI-compatible backend

## 🔒 Security And Privacy

Swobu is local-first.  
It runs on your machine, binds to loopback by default, and keeps control in your hands.

By default, Swobu does **not** send or store:
- prompts or completions
- request/response bodies
- auth headers or API keys

Telemetry is opt-out and aggregate-only:
- usage and reliability counters
- bounded error signals (status/route/operation/duration)
- no raw stack traces unless debug mode is explicitly enabled

```bash
swobu telemetry status
swobu telemetry off
```