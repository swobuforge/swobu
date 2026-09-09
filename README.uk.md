# [Swobu](https://swobu.com/)

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Português (Brasil)](README.pt-BR.md) · [Bahasa Indonesia](README.id.md) · [한국어](README.ko.md) · [Русский](README.ru.md) · [Español](README.es.md) · **Українська**

**Єдиний endpoint для Claude Code, Codex та інших AI-агентів: DeepSeek, Kimi, GLM, OpenAI, Anthropic, OpenRouter, Ollama, Bedrock та інші — з автоматичним роутингом, балансуванням навантаження та failover.**

Зробіть обчислювальні ресурси AI маршрутизованими. Ваш агент лише запитує назву моделі; Swobu перетворює її на route крізь провайдерів, акаунти, регіони та локальні сервери — із балансуванням, failover, трансляцією reasoning і семантичною сумісністю протоколів на рівні платформи.

[Документація](https://swobu.com/docs/) · [Швидкий старт](https://swobu.com/docs/start/first-route/) · [Релізи](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Агенти та клієнти, що підтримуються Swobu" width="900">
  <img src="./assets/readme/providers.png" alt="Провайдери, що підтримуються Swobu" width="1100">
</p>

---

## Ваш агент обирає модель. Swobu обирає, де вона виконується.

Для вашого агента **route Swobu виглядає як звичайна модель**.

За цією назвою може стояти один endpoint, та сама модель у кількох місцях або спільний пул провайдерів.

Схеми нижче — це приклади конфігурації. Обирайте моделі, доступні у ваших провайдерів.

```text
claude-opus-5
    │
    ├─ Anthropic / claude-opus-5
    ├─ AWS Bedrock / account A / claude-opus-5
    └─ AWS Bedrock / account B / claude-opus-5
```

Продовжуйте використовувати `claude-opus-5`. Swobu балансуватиме навантаження та виконуватиме failover за цією назвою.

Або дозвольте назві моделі описувати задачу:

```text
codex-auto-review
    │
    ├─ Deepseek / Deepseek V4 Flash
    ├─ Google / Gemini 3.7 Flash
    └─ another review model
```

Або сформуйте пул, що навмисно поєднує різні моделі та провайдерів:

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

Поле `model`, яке ваш агент уже розуміє, стає програмованою межею маршрутизації.

---

## Почніть однією командою

macOS, Linux або WSL:

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://swobu.com/install.ps1 | iex
```

Інсталятор запустить Swobu та відкриє **Cockpit** — термінальний інтерфейс (TUI).

Додайте провайдера, створіть route та підключіть свого агента.

### Підключення агента

Cockpit може налаштувати підтримувані клієнти автоматично.

Або скористайтеся CLI:

```bash
swobu connect claude
swobu connect codex
swobu connect openclaw
swobu connect pi
swobu connect kilo
swobu connect hermes
```

Після цього ваш агент спілкується зі Swobu. Конфігурація провайдерів та політики маршрутизації залишаються за шлюзом.

[Швидкий старт за 5 хвилин →](https://swobu.com/docs/start/first-route/)

---

## Що змінюється, коли назва моделі стає маршрутом?

### Об'єднуйте потужності в пули

Target — це не просто модель.

Він може представляти конкретний:

- провайдер
- акаунт
- хмарний регіон
- hosted endpoint
- локальний сервер
- модель

Розмістіть кілька targets в одному рівні (tier), щоб балансувати навантаження між ними.

Додайте fallback tiers, щоб визначити поведінку, коли бажані ресурси недоступні.

```text
route: gpt-5.6-sol

primary
├─ Azure / westcentralus / gpt-5.6-sol
└─ Azure / westus2 / gpt-5.6-sol

fallback
└─ OpenAI / gpt-5.6-sol
```

Агент продовжує запитувати `gpt-5.6-sol`.

---

### Маршрутизація між провайдерами

Маршрути не зобов'язані зберігати ідентичність моделі.

Назва на кшталт `review`, `cheap`, `free` або `codex-auto-review` може представляти будь-які ресурси, що мають сенс для цього навантаження.

```text
review
├─ Z.AI / GLM-5.3
├─ Kimi / Kimi-2.8
└─ Ollama / Qwen3-Coder
```

Це дозволяє різним агентам користуватися спільною політикою маршрутизації без жорсткого кодування налаштувань провайдерів у кожному з них.

---

### Failover без переналаштування агента

Вичерпано квоту. Регіон недоступний. Endpoint не відповідає. Акаунт досяг ліміту.

Swobu автоматично спробує наступний відповідний target згідно з маршрутом.

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

Назва маршруту залишається незмінною.

---

## Одна межа, кілька протоколів

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

Наразі Swobu підтримує інтеграції з провайдерами за такими протоколами:

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages
- Gemini Interactions

Точна підтримка протоколів і можливостей залежить від конкретного провайдера.

[Матриця можливостей →](https://swobu.com/docs/)

---

## Провайдери

Swobu підтримує локальний інференс, frontier API, гіперскейлери, спеціалізовані платформи інференсу та агрегатори.

[Перелік провайдерів та інструкції з налаштування доступні в документації.](https://swobu.com/docs/)

---

## Приклади

### Одна модель, кілька провайдерів

Зберігайте назву моделі, яку вже використовує агент, додаючи резервні потужності під нею.

### Безкоштовний міжпровайдерний пул

Об'єднайте періодично доступні безкоштовні ресурси за однією назвою моделі.

### Спершу локально, хмара за потреби

Надавайте пріоритет Ollama, LM Studio або vLLM і перемикайтеся на хмарні ресурси відповідно до політики.

### Маршрути для конкретних агентів

Використовуйте назви на кшталт `codex-auto-review` або `claude-plan`, незалежно змінюючи провайдерів і моделі за ними.

---

## Local-first

Swobu працює локально та надає endpoint, до якого підключаються ваші агенти.

Ваші облікові дані провайдерів залишаються на шлюзі та не копіюються в кожен клієнт.

Для локального використання обліковий запис Swobu не потрібен.

Операційна телеметрія навмисно обмежена та може бути вимкнена.

[Безпека та конфіденційність →](https://swobu.com/docs/)

---

## Релізи

Swobu публікує версіоновані бінарні файли для Linux, macOS та Windows з контрольними сумами SHA-256.

[Останній реліз →](https://github.com/swobuforge/swobu/releases/latest)

Збірка з вихідного коду:

```bash
git clone https://github.com/swobuforge/swobu.git
cd swobu
make build
./.out/swobu --version
```

---

<p align="center">
  <strong>Одна назва моделі. Будь-які потужності під нею.</strong>
</p>

<p align="center">
  <a href="https://swobu.com/docs/start/first-route/">Почати</a>
  ·
  <a href="https://swobu.com/docs/">Документація</a>
  ·
  <a href="https://github.com/swobuforge/swobu/releases">Релізи</a>
</p>
