# Swobu

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Português (Brasil)](README.pt-BR.md) · [Bahasa Indonesia](README.id.md) · [한국어](README.ko.md) · [Русский](README.ru.md) · **Español** · [Українська](README.uk.md)

**Un solo endpoint para que Claude Code, Codex y otros agentes de IA usen DeepSeek, Kimi, GLM, OpenAI, Anthropic, OpenRouter, Ollama, Bedrock y más, con routing, balanceo y failover automáticos.**

Convierte la capacidad de IA en un recurso enrutable. Tu agente pide un modelo; Swobu convierte ese nombre en una route entre proveedores, cuentas, regiones y servidores locales, y se encarga por debajo del balanceo, el failover, la traducción de reasoning y la compatibilidad semántica entre protocolos.

<p align="center">
  <img src="./assets/readme/free-demo.gif" alt="Un agente de IA usando una route de Swobu con balanceo y failover" width="1280">
</p>

[Documentación](https://swobu.com/docs/) · [Inicio rápido](https://swobu.com/docs/start/first-route/) · [Releases](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Agentes y clientes compatibles con Swobu" width="900">
  <img src="./assets/readme/providers.png" alt="Proveedores compatibles con Swobu" width="1100">
</p>

---

## Tu agente elige el modelo. Swobu elige dónde se ejecuta.

Para el agente, una **route de Swobu parece un modelo**.

Detrás de ese nombre puede haber un único endpoint, el mismo modelo disponible en varios lugares o un pool que cruza distintos proveedores.

```text
claude-opus-5
    │
    ├─ Anthropic / claude-opus-5
    ├─ AWS Bedrock / account A / claude-opus-5
    └─ AWS Bedrock / account B / claude-opus-5
```

Sigue usando `claude-opus-5`. Swobu puede balancear capacidad y hacer failover por debajo.

También puedes hacer que el nombre del modelo describa un trabajo:

```text
codex-auto-review
    │
    ├─ Deepseek / Deepseek V4 Flash
    ├─ Google / Gemini 3.7 Flash
    └─ another review model
```

O crear un pool que mezcle deliberadamente modelos y proveedores:

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

El campo `model` que tu agente ya entiende se convierte en una frontera de routing programable.

---

## Empieza con un solo comando

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

El instalador inicia Swobu y abre **Cockpit**, la interfaz de terminal.

Añade un proveedor, crea una route y conecta tu agente.

### Conecta un agente

Cockpit puede configurar por ti los clientes compatibles.

O usa la CLI:

```bash
swobu connect claude
swobu connect codex
swobu connect openclaw
swobu connect pi
swobu connect kilo
swobu connect hermes
```

A partir de ahí, el agente habla con Swobu. La configuración de proveedores y el routing quedan detrás del gateway.

[Inicio rápido en 5 minutos →](https://swobu.com/docs/start/first-route/)

---

## ¿Qué cambia cuando el nombre del modelo se convierte en una route?

### Agrupa capacidad

Un target no es solo un modelo.

Puede representar un determinado:

- proveedor
- cuenta
- región cloud
- endpoint alojado
- servidor local
- modelo

Pon varios targets en el mismo tier para balancear carga entre ellos.

Añade tiers de fallback para definir qué ocurre cuando la capacidad preferida no está disponible.

```text
route: gpt-5.6-sol

primary
├─ Azure / westcentralus / gpt-5.6-sol
└─ Azure / westus2 / gpt-5.6-sol

fallback
└─ OpenAI / gpt-5.6-sol
```

El agente sigue pidiendo `gpt-5.6-sol`.

---

### Routing entre proveedores

Una route no tiene por qué conservar la identidad del modelo.

Un nombre como `review`, `cheap`, `free` o `codex-auto-review` puede representar cualquier capacidad que tenga sentido para ese trabajo.

```text
review
├─ Z.AI / GLM-5.3
├─ Kimi / Kimi-2.8
└─ Ollama / Qwen3-Coder
```

Así distintos agentes pueden compartir una misma política de routing sin incrustar la configuración de proveedores en cada cliente.

---

### Failover sin reconfigurar el agente

Cuota agotada. Región no disponible. Endpoint caído. La cuenta llegó a su límite.

Swobu puede probar el siguiente target elegible según la route.

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

El nombre de la route no cambia.

---

### La intención de reasoning viaja con la petición

Cada proveedor expone reasoning de forma distinta.

Una API puede aceptar un effort level. Otra puede exponer un token budget. Otra puede codificar reasoning mediante una estructura de petición completamente distinta.

Swobu trata reasoning como una capacidad semántica y lo traduce cuando existe una representación equivalente que conserve el significado.

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

No deberías tener que enseñar a cada agente el dialecto de API de cada proveedor.

---

### La compatibilidad hace posible el routing

Enviar el mismo JSON a otra URL es fácil.

Mover de forma segura una petición de agente entre APIs no lo es.

Los proveedores difieren en:

- tools y function calls
- reasoning
- web search
- streaming
- message history
- structured content
- model discovery
- capacidades nativas del proveedor
- detalles de protocolo y edge cases

Swobu traduce las peticiones cuando puede preservar su significado.

Los targets que no pueden representar la semántica requerida pueden excluirse en lugar de degradar silenciosamente la petición.

La compatibilidad no debería ser algo en lo que tengas que pensar continuamente. Es la infraestructura que hace fiable el routing.

---

## Una frontera, varios protocolos

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

Swobu admite actualmente integraciones de proveedores a través de protocolos que incluyen:

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages
- Gemini Interactions

El soporte exacto de protocolos y capacidades varía según el proveedor.

[Matriz de capacidades →](https://swobu.com/docs/)

---

## Proveedores

Swobu soporta inferencia local, APIs frontier, hyperscalers, plataformas especializadas de inferencia y agregadores.

**Local:** Ollama · LM Studio · vLLM

**Frontier:** OpenAI · ChatGPT · Anthropic · Gemini · Mistral · DeepSeek · Kimi · StepFun · Z.AI

**Cloud:** AWS Bedrock · Azure AI · Cloudflare Workers AI · Scaleway · OVHcloud

**Inference:** Cerebras · Groq · SambaNova · NVIDIA NIM · Together AI · Fireworks AI · FriendliAI · DeepInfra · Runpod · Nebius · GMI Cloud · Novita AI · SiliconFlow · Baseten · Hyperbolic · ModelScope · LLM7

**Aggregation:** OpenRouter · Custom Endpoint

El catálogo, el número de proveedores, la matriz de protocolos y los assets del README se generan a partir del provider registry de Swobu.

---

## Las capacidades nativas siguen siendo nativas

Swobu no reduce todos los proveedores al mínimo común denominador.

Cuando un target seleccionado expone una capacidad nativa útil que Swobu entiende, esa capacidad puede seguir disponible a través de la frontera de compatibilidad.

Esto incluye capacidades como el web search nativo del proveedor cuando está soportado.

El principio es simple:

> **preserva la semántica útil cuando sea posible; falla de forma explícita cuando no pueda representarse.**

---

## Construido contra incompatibilidades reales

Swobu existe porque “OpenAI-compatible” suele dejar de ser compatible justo cuando los agentes empiezan a hacer cosas interesantes.

Se prueba contra fallos reales relacionados con:

- reasoning controls
- tool definitions
- campos malformed o unsupported
- message replay
- model discovery
- comportamiento de streaming
- traducción entre protocolos
- restricciones específicas del proveedor

[Notas de compatibilidad →](https://swobu.com/docs/)

---

## Ejemplos

### Mismo modelo, varios proveedores

Mantén el nombre de modelo que el agente ya utiliza y añade capacidad redundante por detrás.

### Pool gratuito entre proveedores

Combina capacidad gratuita recurrente detrás de un único nombre de modelo.

### Local primero, cloud cuando haga falta

Prioriza Ollama, LM Studio o vLLM y pasa a capacidad alojada según la política.

### Routes específicas por agente

Expón nombres como `codex-auto-review` o `claude-plan` mientras cambias de forma independiente los proveedores y modelos que hay detrás.

---

## Local-first

Swobu se ejecuta localmente y expone el endpoint al que se conectan tus agentes.

Las credenciales de los proveedores permanecen en el gateway en vez de copiarse a cada cliente.

No necesitas una cuenta de Swobu para usarlo localmente.

La telemetría operativa se limita deliberadamente y puede desactivarse.

[Seguridad y privacidad →](https://swobu.com/docs/)

---

## Releases

Swobu publica binarios versionados para Linux, macOS y Windows con checksums SHA-256.

[Última release →](https://github.com/swobuforge/swobu/releases/latest)

Instala desde el código fuente:

```bash
go install github.com/swobuforge/swobu/cmd/swobu@latest
```

---

## Estado

El routing, el comportamiento de compatibilidad y las integraciones con proveedores siguen evolucionando mientras se estabilizan las abstracciones.

Se agradecen los informes de bugs y de incompatibilidades.

[Abrir una issue →](https://github.com/swobuforge/swobu/issues)

---

<details>
<summary><strong>OpenAI Build Week 2026</strong></summary>
La arquitectura actual de Swobu se reconstruyó durante OpenAI Build Week 2026 usando GPT 5.6 Sol.
</details>

---

<p align="center">
  <strong>Un nombre de modelo. Cualquier capacidad por detrás.</strong>
</p>

<p align="center">
  <a href="https://swobu.com/docs/start/first-route/">Empezar</a>
  ·
  <a href="https://swobu.com/docs/">Docs</a>
  ·
  <a href="https://github.com/swobuforge/swobu/releases">Releases</a>
</p>
