# [Swobu](https://swobu.com/) — LLM Switchboard

[English](README.md) · **简体中文** · [日本語](README.ja.md) · [Português (Brasil)](README.pt-BR.md) · [Bahasa Indonesia](README.id.md) · [한국어](README.ko.md) · [Русский](README.ru.md) · [Español](README.es.md) · [Українська](README.uk.md)

**一个端点，让 Claude Code、Codex 和其他 AI Agent 在 DeepSeek、Kimi、GLM、OpenAI、Anthropic、OpenRouter、Ollama、Bedrock 等模型与提供商之间自动路由、负载均衡和故障切换。**

让 AI 算力变得可路由。你的 Agent 只需要请求一个模型名；Swobu 会把这个名字变成一条跨提供商、账号、区域和本地服务器的路由，并在底层处理负载均衡、故障切换，以及在请求语义可表示时进行协议转换。

[文档](https://swobu.com/docs/) · [快速开始](https://swobu.com/docs/start/first-route/) · [版本发布](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Swobu 支持的 Agent 和客户端" width="900">
  <img src="./assets/readme/providers.png" alt="Swobu 支持的模型提供商" width="1100">
</p>

---

## Agent 选择模型，Swobu 选择它在哪里运行

对 Agent 来说，Swobu 的**路由看起来就是一个模型**。

这个名字背后可以是单个端点、分布在多个位置的同一个模型，也可以是跨提供商的容量池。

以下示意图展示配置示例。请选择各提供商实际可用的模型。

```text
claude-opus-5
    │
    ├─ Anthropic / claude-opus-5
    ├─ AWS Bedrock / account A / claude-opus-5
    └─ AWS Bedrock / account B / claude-opus-5
```

Agent 继续使用 `claude-opus-5`。Swobu 可以在背后均衡容量并自动故障切换。

也可以让模型名表达一个任务：

```text
codex-auto-review
    │
    ├─ Deepseek / Deepseek V4 Flash
    ├─ Google / Gemini 3.7 Flash
    └─ another review model
```

或者故意把不同模型、不同提供商组合成一个池：

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

Agent 本来就理解的 `model` 字段，因此变成了一个可编程的路由边界。

---

## 一条命令开始

macOS、Linux 或 WSL：

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://swobu.com/install.ps1 | iex
```

安装程序会启动 Swobu，并打开终端 UI **Cockpit**。

添加提供商、创建路由，然后连接你的 Agent。

### 连接 Agent

Cockpit 可以自动配置受支持的客户端。

也可以直接使用 CLI：

```bash
swobu connect claude
swobu connect codex
swobu connect openclaw
swobu connect pi
swobu connect kilo
swobu connect hermes
```

之后，Agent 只与 Swobu 通信。提供商配置和路由策略都留在网关后面。

[5 分钟快速开始 →](https://swobu.com/docs/start/first-route/)

---

## 当模型名变成路由，会发生什么？

### 汇聚容量

一个 target 不只是一个模型。

它可以表示特定的：

- 提供商
- 账号
- 云区域
- 托管端点
- 本地服务器
- 模型

把多个 target 放进同一个 tier，就能在它们之间做负载均衡。

再增加 fallback tier，定义首选容量不可用时该去哪里。

```text
route: gpt-5.6-sol

primary
├─ Azure / westcentralus / gpt-5.6-sol
└─ Azure / westus2 / gpt-5.6-sol

fallback
└─ OpenAI / gpt-5.6-sol
```

Agent 依然只请求 `gpt-5.6-sol`。

---

### 跨提供商路由

路由不必保持模型身份不变。

`review`、`cheap`、`free` 或 `codex-auto-review` 这样的名字，可以代表任何适合这类工作的容量。

```text
review
├─ Z.AI / GLM-5.3
├─ Kimi / Kimi-2.8
└─ Ollama / Qwen3-Coder
```

这样，不同 Agent 可以共享同一套路由策略，而不用把提供商配置硬编码到每个客户端里。

---

### 故障切换，不重新配置 Agent

配额耗尽。区域不可用。端点故障。账号触发限制。

Swobu 会向已配置的 target 发出真实请求。如果该尝试失败，它会继续尝试路由中的下一个 target；Swobu 不会预先判断兼容性。

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

路由名不需要改变。

---

## 一个边界，多种协议

```text
Claude Code ─┐
Codex ───────┤
OpenClaw ────┤
Pi ──────────┤
Kilo ────────┼──── Swobu ────┬─ OpenAI
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

Swobu 当前支持跨以下协议的提供商集成：

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages
- Gemini Interactions

具体协议和能力支持会因提供商而异。

[能力矩阵 →](https://swobu.com/docs/)

---

## 提供商

Swobu 支持本地推理、前沿模型 API、超大规模云平台、专业推理平台和聚合器。

[请参阅文档中的提供商列表和配置说明。](https://swobu.com/docs/)

---

## 示例

### 同一个模型，多个提供商

Agent 保持原来的模型名，同时在背后增加冗余容量。

### 跨提供商免费池

把多个持续提供免费额度的容量合并到一个模型名后面。

### 本地优先，必要时上云

优先使用 Ollama、LM Studio 或 vLLM，再按照策略回退到托管容量。

### Agent 专用路由

暴露 `codex-auto-review`、`claude-plan` 这样的名字，同时独立调整它背后的提供商和模型。

---

## Local-first

Swobu 在本地运行，并暴露 Agent 所连接的端点。

提供商凭据留在网关里，不需要复制到每一个客户端。

本地使用不需要 Swobu 账号。

运行遥测被刻意限制，并且可以关闭。

[安全与隐私 →](https://swobu.com/docs/)

---

## 发布版本

Swobu 为 Linux、macOS 和 Windows 发布带版本号的二进制文件，并提供 SHA-256 校验值。

[最新版本 →](https://github.com/swobuforge/swobu/releases/latest)

从源码构建：

```bash
git clone https://github.com/swobuforge/swobu.git
cd swobu
make build
./.out/swobu --version
```

---

<p align="center">
  <strong>一个模型名。背后可以是任何容量。</strong>
</p>

<p align="center">
  <a href="https://swobu.com/docs/start/first-route/">开始使用</a>
  ·
  <a href="https://swobu.com/docs/">文档</a>
  ·
  <a href="https://github.com/swobuforge/swobu/releases">版本发布</a>
</p>
