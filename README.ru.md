# Swobu

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Português (Brasil)](README.pt-BR.md) · [Bahasa Indonesia](README.id.md) · [한국어](README.ko.md) · **Русский** · [Español](README.es.md) · [Українська](README.uk.md)

**Один endpoint для Claude Code, Codex и других AI-агентов: DeepSeek, Kimi, GLM, OpenAI, Anthropic, OpenRouter, Ollama, Bedrock и другие — с автоматическим роутингом, балансировкой и failover.**

Сделайте AI-вычисления маршрутизируемым ресурсом. Агент запрашивает имя модели; Swobu превращает его в route через провайдеров, аккаунты, регионы и локальные серверы, а под капотом выполняет балансировку, failover, преобразование reasoning и семантическую совместимость протоколов.

<p align="center">
  <img src="./assets/readme/free-demo.gif" alt="AI-агент использует route Swobu с балансировкой и failover" width="1280">
</p>

[Документация](https://swobu.com/docs/) · [Быстрый старт](https://swobu.com/docs/start/first-route/) · [Релизы](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Агенты и клиенты, поддерживаемые Swobu" width="900">
  <img src="./assets/readme/providers.png" alt="Провайдеры, поддерживаемые Swobu" width="1100">
</p>

---

## Агент выбирает модель. Swobu выбирает, где она будет запущена.

Для агента **route Swobu выглядит как модель**.

За этим именем может стоять один endpoint, одна и та же модель в нескольких местах или pool из разных провайдеров.

```text
claude-opus-5
    │
    ├─ Anthropic / claude-opus-5
    ├─ AWS Bedrock / account A / claude-opus-5
    └─ AWS Bedrock / account B / claude-opus-5
```

Продолжайте использовать `claude-opus-5`. Swobu может балансировать доступную мощность и делать failover за этим именем.

Или пусть имя модели обозначает задачу:

```text
codex-auto-review
    │
    ├─ Deepseek / Deepseek V4 Flash
    ├─ Google / Gemini 3.7 Flash
    └─ another review model
```

Можно и намеренно собрать в одном pool разные модели и разных провайдеров:

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

Поле `model`, которое агент уже понимает, становится программируемой границей роутинга.

---

## Старт одной командой

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Установщик запускает Swobu и открывает **Cockpit** — терминальный интерфейс.

Добавьте провайдера, создайте route и подключите агента.

### Подключить агента

Cockpit умеет сам настраивать поддерживаемые клиенты.

Или используйте CLI:

```bash
swobu connect claude
swobu connect codex
swobu connect openclaw
swobu connect pi
swobu connect kilo
swobu connect hermes
```

После этого агент общается со Swobu. Настройки провайдеров и маршрутизация остаются за gateway.

[Быстрый старт за 5 минут →](https://swobu.com/docs/start/first-route/)

---

## Что меняется, когда имя модели становится route?

### Объединяйте вычислительные ресурсы

Target — это не просто модель.

Он может обозначать конкретный:

- провайдер
- аккаунт
- облачный регион
- hosted endpoint
- локальный сервер
- модель

Поместите несколько targets в один tier, чтобы балансировать нагрузку между ними.

Добавьте fallback tiers, чтобы определить, что делать, когда приоритетные вычислительные ресурсы недоступны.

```text
route: gpt-5.6-sol

primary
├─ Azure / westcentralus / gpt-5.6-sol
└─ Azure / westus2 / gpt-5.6-sol

fallback
└─ OpenAI / gpt-5.6-sol
```

Агент по-прежнему запрашивает `gpt-5.6-sol`.

---

### Роутинг между провайдерами

Route не обязан сохранять идентичность модели.

Имя вроде `review`, `cheap`, `free` или `codex-auto-review` может обозначать любые вычислительные ресурсы, подходящие для этой задачи.

```text
review
├─ Z.AI / GLM-5.3
├─ Kimi / Kimi-2.8
└─ Ollama / Qwen3-Coder
```

Так разные агенты могут использовать общую политику маршрутизации без hardcode настроек провайдеров в каждом клиенте.

---

### Failover без перенастройки агента

Закончилась квота. Недоступен регион. Упал endpoint. Аккаунт достиг лимита.

Swobu может попробовать следующий подходящий target согласно route.

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

Имя route не меняется.

---

### Намерение reasoning путешествует вместе с запросом

Разные провайдеры выражают reasoning по-разному.

Один API принимает effort level. Другой открывает token budget. Третий кодирует reasoning совершенно иной формой запроса.

Swobu рассматривает reasoning как семантическую capability и преобразует его там, где существует осмысленное эквивалентное представление.

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

Вам не должно приходиться обучать каждый агент диалекту API каждого провайдера.

---

### Совместимость делает роутинг возможным

Отправить тот же JSON на другой URL легко.

Безопасно перенести запрос агента между API — нет.

Провайдеры расходятся в том, как они реализуют:

- tools и function calls
- reasoning
- web search
- streaming
- message history
- structured content
- model discovery
- нативные capability провайдера
- детали протокола и edge cases

Swobu преобразует запросы, когда может сохранить их смысл.

Targets, которые не способны представить необходимую семантику, можно исключить вместо того, чтобы молча ухудшать запрос.

Совместимость — не та часть продукта, о которой вы должны постоянно думать. Это инфраструктура, делающая роутинг надёжным.

---

## Одна граница, несколько протоколов

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

Сейчас Swobu поддерживает интеграции провайдеров через протоколы, включая:

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages
- Gemini Interactions

Точная поддержка протоколов и capabilities зависит от провайдера.

[Матрица capabilities →](https://swobu.com/docs/)

---

## Провайдеры

Swobu поддерживает локальный inference, frontier API, hyperscalers, специализированные inference-платформы и агрегаторы.

**Local:** Ollama · LM Studio · vLLM

**Frontier:** OpenAI · ChatGPT · Anthropic · Gemini · Mistral · DeepSeek · Kimi · StepFun · Z.AI

**Cloud:** AWS Bedrock · Azure AI · Cloudflare Workers AI · Scaleway · OVHcloud

**Inference:** Cerebras · Groq · SambaNova · NVIDIA NIM · Together AI · Fireworks AI · FriendliAI · DeepInfra · Runpod · Nebius · GMI Cloud · Novita AI · SiliconFlow · Baseten · Hyperbolic · ModelScope · LLM7

**Aggregation:** OpenRouter · Custom Endpoint

Каталог, число провайдеров, матрица протоколов и README assets генерируются из provider registry Swobu.

---

## Нативные capabilities остаются нативными

Swobu не сводит всех провайдеров к наименьшему общему знаменателю.

Если выбранный target предоставляет полезную native capability, которую Swobu понимает, она может оставаться доступной через compatibility boundary.

Например, provider-native web search там, где он поддерживается.

Принцип прост:

> **сохранять полезную семантику, когда это возможно; явно падать, когда её нельзя представить.**

---

## Построено против реальных несовместимостей

Swobu существует потому, что “OpenAI-compatible” часто перестаёт быть совместимым ровно там, где агенты начинают делать что-то интересное.

Swobu тестируется на реальных сбоях, связанных с:

- reasoning controls
- tool definitions
- malformed или unsupported fields
- message replay
- model discovery
- streaming behavior
- cross-protocol translation
- provider-specific request restrictions

[Заметки о совместимости →](https://swobu.com/docs/)

---

## Примеры

### Одна модель, несколько провайдеров

Сохраните имя модели, которое уже использует агент, но добавьте за ним резервные вычислительные ресурсы.

### Бесплатный pool между провайдерами

Объедините регулярно доступную бесплатные вычислительные ресурсы за одним именем модели.

### Сначала local, cloud при необходимости

Отдавайте приоритет Ollama, LM Studio или vLLM и переходите на облачные вычислительные ресурсы по заданной политике.

### Routes для конкретных агентов

Публикуйте имена вроде `codex-auto-review` или `claude-plan`, независимо меняя провайдеров и модели за ними.

---

## Local-first

Swobu работает локально и предоставляет endpoint, к которому подключаются ваши агенты.

Учётные данные провайдеров остаются на gateway, а не копируются в каждый клиент.

Для локального использования аккаунт Swobu не нужен.

Операционная telemetry намеренно ограничена и может быть отключена.

[Безопасность и приватность →](https://swobu.com/docs/)

---

## Релизы

Swobu публикует версионированные бинарники для Linux, macOS и Windows с SHA-256 checksums.

[Последний релиз →](https://github.com/swobuforge/swobu/releases/latest)

Установка из исходников:

```bash
go install github.com/swobuforge/swobu/cmd/swobu@latest
```

---

## Статус

Роутинг, поведение совместимости и интеграции провайдеров продолжают развиваться, пока стабилизируются абстракции.

Bug reports и compatibility reports приветствуются.

[Открыть issue →](https://github.com/swobuforge/swobu/issues)

---

<details>
<summary><strong>OpenAI Build Week 2026</strong></summary>
Текущая архитектура Swobu была перестроена во время OpenAI Build Week 2026 с использованием GPT 5.6 Sol.
</details>

---

<p align="center">
  <strong>Одно имя модели. Любые вычислительные ресурсы за ним.</strong>
</p>

<p align="center">
  <a href="https://swobu.com/docs/start/first-route/">Начать</a>
  ·
  <a href="https://swobu.com/docs/">Документация</a>
  ·
  <a href="https://github.com/swobuforge/swobu/releases">Релизы</a>
</p>
