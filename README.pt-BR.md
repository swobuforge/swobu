# [Swobu](https://swobu.com/)

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · **Português (Brasil)** · [Bahasa Indonesia](README.id.md) · [한국어](README.ko.md) · [Русский](README.ru.md) · [Español](README.es.md) · [Українська](README.uk.md)

**Um único endpoint para Claude Code, Codex e outros agentes de IA usarem DeepSeek, Kimi, GLM, OpenAI, Anthropic, OpenRouter, Ollama, Bedrock e mais — com roteamento, balanceamento e failover automáticos.**

Transforme capacidade de IA em algo roteável. Seu agente pede um modelo; o Swobu transforma esse nome em uma rota entre provedores, contas, regiões e servidores locais, cuidando por baixo dos panos de balanceamento, failover, tradução de reasoning e compatibilidade semântica entre protocolos.

[Documentação](https://swobu.com/docs/) · [Início rápido](https://swobu.com/docs/start/first-route/) · [Releases](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Agentes e clientes compatíveis com Swobu" width="900">
  <img src="./assets/readme/providers.png" alt="Provedores compatíveis com Swobu" width="1100">
</p>

---

## Seu agente escolhe o modelo. O Swobu escolhe onde ele roda.

Para o agente, uma **rota do Swobu parece um modelo**.

Por trás desse nome pode existir um único endpoint, o mesmo modelo disponível em vários lugares ou um pool que cruza diferentes provedores.

Os diagramas são exemplos de configuração. Escolha os modelos disponíveis nos seus provedores.

```text
claude-opus-5
    │
    ├─ Anthropic / claude-opus-5
    ├─ AWS Bedrock / account A / claude-opus-5
    └─ AWS Bedrock / account B / claude-opus-5
```

Continue usando `claude-opus-5`. O Swobu pode balancear a capacidade e fazer failover por baixo dele.

Ou faça o nome do modelo representar um trabalho:

```text
codex-auto-review
    │
    ├─ Deepseek / Deepseek V4 Flash
    ├─ Google / Gemini 3.7 Flash
    └─ another review model
```

Você também pode criar um pool que misture deliberadamente modelos e provedores:

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

O campo `model` que seu agente já entende vira uma fronteira de roteamento programável.

---

## Comece com um comando

macOS, Linux ou WSL:

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://swobu.com/install.ps1 | iex
```

O instalador inicia o Swobu e abre o **Cockpit**, a interface de terminal.

Adicione um provedor, crie uma rota e conecte seu agente.

### Conecte um agente

O Cockpit pode configurar clientes compatíveis para você.

Ou use a CLI:

```bash
swobu connect claude
swobu connect codex
swobu connect openclaw
swobu connect pi
swobu connect kilo
swobu connect hermes
```

Depois disso, seu agente conversa com o Swobu. Configuração de provedores e roteamento ficam atrás do gateway.

[Início rápido em 5 minutos →](https://swobu.com/docs/start/first-route/)

---

## O que muda quando o nome do modelo vira uma rota?

### Agrupe capacidade

Um target não é apenas um modelo.

Ele pode representar um determinado:

- provedor
- conta
- região de nuvem
- endpoint hospedado
- servidor local
- modelo

Coloque vários targets no mesmo tier para balancear carga entre eles.

Adicione tiers de fallback para definir o que acontece quando a capacidade preferida fica indisponível.

```text
route: gpt-5.6-sol

primary
├─ Azure / westcentralus / gpt-5.6-sol
└─ Azure / westus2 / gpt-5.6-sol

fallback
└─ OpenAI / gpt-5.6-sol
```

O agente continua pedindo `gpt-5.6-sol`.

---

### Roteie entre provedores

Uma rota não precisa preservar a identidade do modelo.

Um nome como `review`, `cheap`, `free` ou `codex-auto-review` pode representar qualquer capacidade adequada para aquela tarefa.

```text
review
├─ Z.AI / GLM-5.3
├─ Kimi / Kimi-2.8
└─ Ollama / Qwen3-Coder
```

Assim, vários agentes podem compartilhar a mesma política de roteamento sem embutir configuração de provedores em cada cliente.

---

### Faça failover sem reconfigurar o agente

Quota acabou. Região indisponível. Endpoint falhou. A conta bateu no limite.

O Swobu pode tentar o próximo target elegível conforme a rota.

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

O nome da rota não muda.

---

## Uma fronteira, vários protocolos

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

Atualmente, o Swobu integra provedores através de protocolos como:

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages
- Gemini Interactions

O suporte exato a protocolos e capacidades varia por provedor.

[Matriz de capacidades →](https://swobu.com/docs/)

---

## Provedores

O Swobu suporta inferência local, APIs de fronteira, hyperscalers, plataformas especializadas de inferência e agregadores.

[Consulte os provedores e sua configuração na documentação.](https://swobu.com/docs/)

---

## Exemplos

### Mesmo modelo, vários provedores

Mantenha o nome de modelo que o agente já usa e adicione capacidade redundante por trás dele.

### Pool gratuito entre provedores

Combine capacidade gratuita recorrente atrás de um único nome de modelo.

### Local primeiro, nuvem quando necessário

Priorize Ollama, LM Studio ou vLLM e caia para capacidade hospedada conforme a política.

### Rotas específicas por agente

Exponha nomes como `codex-auto-review` ou `claude-plan` enquanto altera independentemente os provedores e modelos por trás deles.

---

## Local-first

O Swobu roda localmente e expõe o endpoint ao qual seus agentes se conectam.

Suas credenciais de provedores ficam no gateway, em vez de serem copiadas para cada cliente.

Nenhuma conta Swobu é necessária para uso local.

A telemetria operacional é deliberadamente limitada e pode ser desativada.

[Segurança e privacidade →](https://swobu.com/docs/)

---

## Releases

O Swobu publica binários versionados para Linux, macOS e Windows, com checksums SHA-256.

[Release mais recente →](https://github.com/swobuforge/swobu/releases/latest)

Compile a partir do código-fonte:

```bash
git clone https://github.com/swobuforge/swobu.git
cd swobu
make build
./.out/swobu --version
```

---

<p align="center">
  <strong>Um nome de modelo. Qualquer capacidade por trás.</strong>
</p>

<p align="center">
  <a href="https://swobu.com/docs/start/first-route/">Começar</a>
  ·
  <a href="https://swobu.com/docs/">Docs</a>
  ·
  <a href="https://github.com/swobuforge/swobu/releases">Releases</a>
</p>
