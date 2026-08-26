# Swobu

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Português (Brasil)](README.pt-BR.md) · [Bahasa Indonesia](README.id.md) · **한국어** · [Русский](README.ru.md) · [Español](README.es.md) · [Українська](README.uk.md)

**Claude Code, Codex 및 다른 AI 에이전트를 하나의 엔드포인트에서 DeepSeek, Kimi, GLM, OpenAI, Anthropic, OpenRouter, Ollama, Bedrock 등으로 연결하고 자동 라우팅, 로드 밸런싱, 페일오버를 적용합니다.**

AI 용량을 라우팅 가능한 자원으로 만듭니다. 에이전트는 모델 이름 하나만 요청합니다. Swobu는 그 이름을 프로바이더, 계정, 리전, 로컬 서버를 아우르는 route로 바꾸고, 그 아래에서 로드 밸런싱, 페일오버, reasoning 변환, 의미 보존형 프로토콜 호환성을 처리합니다.

<p align="center">
  <img src="./assets/readme/free-demo.gif" alt="Swobu route를 통해 로드 밸런싱과 페일오버를 사용하는 AI 에이전트" width="1280">
</p>

[문서](https://swobu.com/docs/) · [빠른 시작](https://swobu.com/docs/start/first-route/) · [릴리스](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Swobu가 지원하는 에이전트와 클라이언트" width="900">
  <img src="./assets/readme/providers.png" alt="Swobu가 지원하는 프로바이더" width="1100">
</p>

---

## 에이전트는 모델을 고르고, Swobu는 실행 위치를 고릅니다

에이전트에게 Swobu의 **route는 하나의 모델처럼 보입니다**.

그 이름 뒤에는 하나의 endpoint, 여러 곳에서 제공되는 동일 모델, 또는 여러 프로바이더를 섞은 pool이 있을 수 있습니다.

```text
claude-opus-5
    │
    ├─ Anthropic / claude-opus-5
    ├─ AWS Bedrock / account A / claude-opus-5
    └─ AWS Bedrock / account B / claude-opus-5
```

계속 `claude-opus-5`를 사용하면 됩니다. Swobu가 뒤에서 용량을 분산하고 필요하면 페일오버합니다.

모델 이름 자체가 작업을 표현하도록 만들 수도 있습니다.

```text
codex-auto-review
    │
    ├─ Deepseek / Deepseek V4 Flash
    ├─ Google / Gemini 3.7 Flash
    └─ another review model
```

서로 다른 모델과 프로바이더를 의도적으로 섞은 pool도 만들 수 있습니다.

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

에이전트가 이미 이해하는 `model` 필드가 그대로 프로그래밍 가능한 라우팅 경계가 됩니다.

---

## 명령어 하나로 시작

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

설치 프로그램이 Swobu를 실행하고 터미널 UI인 **Cockpit**을 엽니다.

프로바이더를 추가하고 route를 만든 뒤 에이전트를 연결하세요.

### 에이전트 연결

Cockpit이 지원되는 클라이언트를 설정할 수 있습니다.

또는 CLI를 사용하세요.

```bash
swobu connect claude
swobu connect codex
swobu connect openclaw
swobu connect pi
swobu connect kilo
swobu connect hermes
```

이후 에이전트는 Swobu와 통신합니다. 프로바이더 설정과 라우팅 정책은 gateway 뒤에 남습니다.

[5분 빠른 시작 →](https://swobu.com/docs/start/first-route/)

---

## 모델 이름이 route가 되면 무엇이 달라지나?

### 용량을 pool로 묶기

target은 단순한 모델이 아닙니다.

다음 중 하나를 나타낼 수 있습니다.

- 프로바이더
- 계정
- 클라우드 리전
- hosted endpoint
- 로컬 서버
- 모델

여러 target을 같은 tier에 두면 그 사이에서 로드 밸런싱할 수 있습니다.

fallback tier를 추가하면 선호 용량을 사용할 수 없을 때 어떻게 할지도 정의할 수 있습니다.

```text
route: gpt-5.6-sol

primary
├─ Azure / westcentralus / gpt-5.6-sol
└─ Azure / westus2 / gpt-5.6-sol

fallback
└─ OpenAI / gpt-5.6-sol
```

에이전트는 여전히 `gpt-5.6-sol`만 요청합니다.

---

### 프로바이더를 넘나드는 라우팅

route가 모델 정체성을 유지할 필요는 없습니다.

`review`, `cheap`, `free`, `codex-auto-review` 같은 이름에 해당 작업에 적합한 어떤 용량이든 연결할 수 있습니다.

```text
review
├─ Z.AI / GLM-5.3
├─ Kimi / Kimi-2.8
└─ Ollama / Qwen3-Coder
```

각 에이전트에 프로바이더 설정을 하드코딩하지 않고도 여러 에이전트가 동일한 라우팅 정책을 공유할 수 있습니다.

---

### 에이전트를 다시 설정하지 않고 페일오버

쿼터 소진. 리전 장애. endpoint 실패. 계정 한도 도달.

Swobu는 route에 따라 다음으로 적합한 target을 시도할 수 있습니다.

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

route 이름은 바뀌지 않습니다.

---

### reasoning 의도도 요청과 함께 이동

프로바이더마다 reasoning을 표현하는 방식이 다릅니다.

어떤 API는 effort level을 받고, 다른 API는 token budget을 노출하며, 또 다른 API는 전혀 다른 request shape로 reasoning을 표현합니다.

Swobu는 reasoning을 의미적 capability로 다루며, 의미를 보존하는 대응 표현이 있을 때 변환합니다.

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

프로바이더를 바꿀 때마다 모든 에이전트에게 새로운 API 방언을 가르칠 필요가 없어야 합니다.

---

### 호환성이 있어야 라우팅이 가능하다

같은 JSON을 다른 URL로 보내는 건 쉽습니다.

에이전트 요청을 API 사이에서 안전하게 옮기는 건 쉽지 않습니다.

프로바이더는 다음 항목에서 서로 다릅니다.

- tools 및 function calls
- reasoning
- web search
- streaming
- message history
- structured content
- model discovery
- 프로바이더 native capability
- 프로토콜 세부 사항과 edge case

Swobu는 의미를 보존할 수 있을 때 요청을 변환합니다.

필요한 의미를 표현할 수 없는 target은 조용히 요청을 열화시키는 대신 후보에서 제외할 수 있습니다.

호환성은 사용자가 매번 고민해야 하는 제품 기능이 아닙니다. 신뢰할 수 있는 라우팅을 가능하게 하는 기반입니다.

---

## 하나의 경계, 여러 프로토콜

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

Swobu는 현재 다음을 포함한 여러 프로토콜의 프로바이더 통합을 지원합니다.

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages
- Gemini Interactions

정확한 프로토콜 및 capability 지원 범위는 프로바이더마다 다릅니다.

[Capability matrix →](https://swobu.com/docs/)

---

## 프로바이더

Swobu는 로컬 추론, frontier API, hyperscaler, 특화 추론 플랫폼, aggregator를 지원합니다.

**Local:** Ollama · LM Studio · vLLM

**Frontier:** OpenAI · ChatGPT · Anthropic · Gemini · Mistral · DeepSeek · Kimi · StepFun · Z.AI

**Cloud:** AWS Bedrock · Azure AI · Cloudflare Workers AI · Scaleway · OVHcloud

**Inference:** Cerebras · Groq · SambaNova · NVIDIA NIM · Together AI · Fireworks AI · FriendliAI · DeepInfra · Runpod · Nebius · GMI Cloud · Novita AI · SiliconFlow · Baseten · Hyperbolic · ModelScope · LLM7

**Aggregation:** OpenRouter · Custom Endpoint

카탈로그, 프로바이더 수, 프로토콜 매트릭스, README asset은 Swobu의 provider registry에서 생성됩니다.

---

## Native capability는 native 그대로

Swobu는 모든 프로바이더를 최저 공통 기능으로 낮추지 않습니다.

선택된 target이 Swobu가 이해하는 유용한 native capability를 제공한다면, 해당 capability를 호환성 경계를 넘어 그대로 사용할 수 있습니다.

지원되는 경우 provider-native web search도 포함됩니다.

원칙은 단순합니다.

> **유용한 의미를 보존할 수 있으면 보존하고, 표현할 수 없으면 명확하게 실패한다.**

---

## 실제 비호환성을 기준으로 구축

Swobu가 존재하는 이유는 “OpenAI-compatible”이 에이전트가 흥미로운 일을 시작하는 지점에서 자주 호환되지 않기 때문입니다.

다음과 같은 실제 실패 사례를 기준으로 테스트합니다.

- reasoning controls
- tool definitions
- malformed 또는 unsupported fields
- message replay
- model discovery
- streaming behavior
- cross-protocol translation
- provider-specific request restrictions

[호환성 노트 →](https://swobu.com/docs/)

---

## 예시

### 같은 모델, 여러 프로바이더

에이전트가 이미 쓰는 모델 이름은 유지하면서 뒤에 중복 용량을 추가합니다.

### 프로바이더를 넘나드는 무료 pool

반복적으로 제공되는 무료 용량을 하나의 모델 이름 뒤에 합칩니다.

### 로컬 우선, 필요할 때 클라우드

Ollama, LM Studio, vLLM을 우선 사용하고 정책에 따라 hosted 용량으로 fallback합니다.

### 에이전트별 route

`codex-auto-review`, `claude-plan` 같은 이름을 노출하면서 그 뒤의 프로바이더와 모델은 독립적으로 변경합니다.

---

## Local-first

Swobu는 로컬에서 실행되며 에이전트가 연결하는 endpoint를 노출합니다.

프로바이더 credential은 각 클라이언트에 복사하지 않고 gateway에 유지합니다.

로컬 사용에 Swobu 계정은 필요하지 않습니다.

운영 telemetry는 의도적으로 제한되어 있고 끌 수 있습니다.

[보안 및 개인정보 보호 →](https://swobu.com/docs/)

---

## 릴리스

Swobu는 Linux, macOS, Windows용 버전 관리 binary와 SHA-256 checksum을 제공합니다.

[최신 릴리스 →](https://github.com/swobuforge/swobu/releases/latest)

소스에서 설치:

```bash
go install github.com/swobuforge/swobu/cmd/swobu@latest
```

---

## 상태

추상화가 안정되는 동안 라우팅, 호환 동작, 프로바이더 통합은 계속 발전하고 있습니다.

버그 및 호환성 리포트를 환영합니다.

[Issue 열기 →](https://github.com/swobuforge/swobu/issues)

---

<details>
<summary><strong>OpenAI Build Week 2026</strong></summary>
Swobu의 현재 아키텍처는 OpenAI Build Week 2026 동안 GPT 5.6 Sol을 사용해 재구축되었습니다.
</details>

---

<p align="center">
  <strong>하나의 모델 이름. 그 뒤에는 어떤 용량이든.</strong>
</p>

<p align="center">
  <a href="https://swobu.com/docs/start/first-route/">시작하기</a>
  ·
  <a href="https://swobu.com/docs/">문서</a>
  ·
  <a href="https://github.com/swobuforge/swobu/releases">릴리스</a>
</p>
