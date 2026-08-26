# Swobu

[English](README.md) · **简体中文** · [日本語](README.ja.md) · [Português (Brasil)](README.pt-BR.md) · [Bahasa Indonesia](README.id.md) · [한국어](README.ko.md) · [Русский](README.ru.md) · [Español](README.es.md) · [Українська](README.uk.md)

**一个端点，让 Claude Code、Codex 和其他 AI Agent 在 DeepSeek、Kimi、GLM、OpenAI、Anthropic、OpenRouter、Ollama、Bedrock 等模型与提供商之间自动路由、负载均衡和故障切换。**

让 AI 算力变得可路由。你的 Agent 只需要请求一个模型名；Swobu 会把这个名字变成一条跨提供商、账号、区域和本地服务器的路由，并在底层处理负载均衡、故障切换、推理语义转换和协议兼容。

<p align="center">
  <img src="./assets/readme/free-demo.gif" alt="AI Agent 通过 Swobu 路由进行负载均衡和故障切换" width="1280">
</p>

[文档](https://swobu.com/docs/) · [快速开始](https://swobu.com/docs/start/first-route/) · [版本发布](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Swobu 支持的 Agent 和客户端" width="900">
  <img src="./assets/readme/providers.png" alt="Swobu 支持的模型提供商" width="1100">
</p>

---

## Agent 选择模型，Swobu 选择它在哪里运行

对 Agent 来说，Swobu 的**路由看起来就是一个模型**。

这个名字背后可以是单个端点、分布在多个位置的同一个模型，也可以是跨提供商的容量池。

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

```bash
curl -fsSL https://swobu.com/install.sh | sh
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

Swobu 会按照路由规则尝试下一个符合条件的 target。

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

### 推理意图跟着请求走

不同提供商表达 reasoning 的方式并不一样。

有的 API 接收 effort level，有的暴露 token budget，还有的用完全不同的请求结构表示推理。

Swobu 把 reasoning 当作语义能力来处理；只要目标 API 存在有意义的等价表达，就进行转换。

```text
agent intent
    │
    │ reasoning: high
    ▼
   Swobu
    │
    ├─ provider A → reasoning effort
    ├─ provider B → reasoning budget
    └─ provider C → native equivalent
```

你不应该为了换一个提供商，就教每个 Agent 一套新的 API 方言。

---

### 兼容性让路由真正可用

把同一份 JSON 发到另一个 URL 很容易。

把 Agent 请求安全地迁移到另一个 API 并不容易。

提供商在这些地方经常不一致：

- tools 和 function calls
- reasoning
- web search
- streaming
- message history
- structured content
- model discovery
- 提供商原生能力
- 协议细节和边界情况

当请求语义可以保留时，Swobu 会进行转换。

无法表达所需语义的 target 可以被直接排除，而不是悄悄降级请求。

兼容性不是你应该反复操心的产品功能。它是让路由值得信任的基础设施。

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

**本地：** Ollama · LM Studio · vLLM

**前沿：** OpenAI · ChatGPT · Anthropic · Gemini · Mistral · DeepSeek · Kimi · StepFun · Z.AI

**云：** AWS Bedrock · Azure AI · Cloudflare Workers AI · Scaleway · OVHcloud

**推理平台：** Cerebras · Groq · SambaNova · NVIDIA NIM · Together AI · Fireworks AI · FriendliAI · DeepInfra · Runpod · Nebius · GMI Cloud · Novita AI · SiliconFlow · Baseten · Hyperbolic · ModelScope · LLM7

**聚合：** OpenRouter · Custom Endpoint

目录、提供商数量、协议矩阵和 README 资源均由 Swobu 的 provider registry 生成。

---

## 原生能力继续保持原生

Swobu 不会为了统一接口，把所有提供商压到最低公分母。

如果某个 target 暴露了 Swobu 能理解的有用原生能力，那么该能力可以继续穿过兼容边界使用。

例如，在受支持的提供商上保留 provider-native web search。

原则很简单：

> **能保留有用语义，就保留；无法表达时，就明确失败。**

---

## 针对真实兼容性问题构建

Swobu 存在的原因，是“OpenAI-compatible”往往恰恰在 Agent 开始变得复杂时不再兼容。

Swobu 针对真实故障进行测试，包括：

- reasoning controls
- tool definitions
- malformed 或 unsupported fields
- message replay
- model discovery
- streaming behavior
- cross-protocol translation
- provider-specific request restrictions

[兼容性说明 →](https://swobu.com/docs/)

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

从源码安装：

```bash
go install github.com/swobuforge/swobu/cmd/swobu@latest
```

---

## 当前状态

随着抽象逐步稳定，路由、兼容性行为和提供商集成仍在持续演进。

欢迎提交 bug 和兼容性问题。

[提交 issue →](https://github.com/swobuforge/swobu/issues)

---

<details>
<summary><strong>OpenAI Build Week 2026</strong></summary>
Swobu 当前架构在 OpenAI Build Week 2026 期间使用 GPT 5.6 Sol 重建。
</details>

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
