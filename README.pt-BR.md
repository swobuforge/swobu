# Swobu

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · **Português (Brasil)** · [Bahasa Indonesia](README.id.md) · [한국어](README.ko.md) · [Русский](README.ru.md) · [Español](README.es.md) · [Українська](README.uk.md)

**Um único endpoint para Claude Code, Codex e outros agentes de IA usarem DeepSeek, Kimi, GLM, OpenAI, Anthropic, OpenRouter, Ollama, Bedrock e mais — com roteamento, balanceamento e failover automáticos.**

Transforme capacidade de IA em algo roteável. Seu agente pede um modelo; o Swobu transforma esse nome em uma rota entre provedores, contas, regiões e servidores locais, cuidando por baixo dos panos de balanceamento, failover, tradução de reasoning e compatibilidade semântica entre protocolos.

<p align="center">
  <img src="./assets/readme/free-demo.gif" alt="Um agente de IA usando uma rota Swobu com balanceamento e failover" width="1280">
</p>

[Documentação](https://swobu.com/docs/) · [Início rápido](https://swobu.com/docs/start/first-route/) · [Releases](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Agentes e clientes compatíveis com Swobu" width="900">
  <img src="./assets/readme/providers.png" alt="Provedores compatíveis com Swobu" width="1100">
</p>

---

## Seu agente escolhe o modelo. O Swobu escolhe onde ele roda.

Para o agente, uma **rota do Swobu parece um modelo**.

Por trás desse nome pode existir um único endpoint, o mesmo modelo disponível em vários lugares ou um pool que cruza diferentes provedores.

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

```bash
curl -fsSL https://swobu.com/install.sh | sh
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

### A intenção de reasoning acompanha a requisição

Cada provedor expõe reasoning de um jeito diferente.

Uma API pode aceitar um effort level. Outra pode expor um token budget. Outra pode codificar reasoning em um formato de requisição completamente diferente.

O Swobu trata reasoning como uma capacidade semântica e traduz quando existe uma representação equivalente que preserve o significado.

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

Você não deveria precisar ensinar a cada agente o dialeto de API de cada provedor.

---

### Compatibilidade torna o roteamento possível

Mandar o mesmo JSON para outra URL é fácil.

Mover com segurança uma requisição de agente entre APIs não é.

Provedores divergem em:

- tools e function calls
- reasoning
- web search
- streaming
- message history
- structured content
- model discovery
- capacidades nativas do provedor
- detalhes de protocolo e edge cases

O Swobu traduz requisições quando consegue preservar o significado delas.

Targets que não conseguem representar a semântica exigida podem ser excluídos, em vez de degradar a requisição silenciosamente.

Compatibilidade não deveria ser algo em que você precisa pensar toda hora. É a infraestrutura que torna o roteamento confiável.

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

**Local:** Ollama · LM Studio · vLLM

**Frontier:** OpenAI · ChatGPT · Anthropic · Gemini · Mistral · DeepSeek · Kimi · StepFun · Z.AI

**Cloud:** AWS Bedrock · Azure AI · Cloudflare Workers AI · Scaleway · OVHcloud

**Inference:** Cerebras · Groq · SambaNova · NVIDIA NIM · Together AI · Fireworks AI · FriendliAI · DeepInfra · Runpod · Nebius · GMI Cloud · Novita AI · SiliconFlow · Baseten · Hyperbolic · ModelScope · LLM7

**Aggregation:** OpenRouter · Custom Endpoint

O catálogo, a contagem de provedores, a matriz de protocolos e os assets do README são gerados a partir do provider registry do Swobu.

---

## Capacidades nativas continuam nativas

O Swobu não reduz todos os provedores ao menor denominador comum.

Quando um target selecionado oferece uma capacidade nativa útil que o Swobu entende, ela pode continuar disponível através da fronteira de compatibilidade.

Isso inclui recursos como web search nativo do provedor, onde houver suporte.

O princípio é simples:

> **preserve semântica útil quando for possível; falhe de forma explícita quando não for.**

---

## Construído contra incompatibilidades reais

O Swobu existe porque “OpenAI-compatible” costuma deixar de ser compatível exatamente quando agentes começam a fazer coisas interessantes.

Ele é testado contra falhas reais envolvendo:

- reasoning controls
- tool definitions
- campos malformados ou não suportados
- message replay
- model discovery
- comportamento de streaming
- tradução entre protocolos
- restrições específicas de provedores

[Notas de compatibilidade →](https://swobu.com/docs/)

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

Instale a partir do código-fonte:

```bash
go install github.com/swobuforge/swobu/cmd/swobu@latest
```

---

## Status

Roteamento, comportamento de compatibilidade e integrações com provedores continuam evoluindo enquanto as abstrações se estabilizam.

Relatórios de bugs e de incompatibilidade são bem-vindos.

[Abrir uma issue →](https://github.com/swobuforge/swobu/issues)

---

<details>
<summary><strong>OpenAI Build Week 2026</strong></summary>
A arquitetura atual do Swobu foi reconstruída durante a OpenAI Build Week 2026 usando GPT 5.6 Sol.
</details>

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
