# Swobu

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Português (Brasil)](README.pt-BR.md) · **Bahasa Indonesia** · [한국어](README.ko.md) · [Русский](README.ru.md) · [Español](README.es.md) · [Українська](README.uk.md)

**Satu endpoint untuk Claude Code, Codex, dan agen AI lain agar dapat memakai DeepSeek, Kimi, GLM, OpenAI, Anthropic, OpenRouter, Ollama, Bedrock, dan lainnya — dengan routing, load balancing, dan failover otomatis.**

Jadikan kapasitas AI dapat dirutekan. Agen Anda meminta sebuah model; Swobu mengubah nama model itu menjadi route lintas provider, akun, region, dan server lokal, sambil menangani load balancing, failover, translasi reasoning, serta kompatibilitas protokol secara semantik di belakang layar.

<p align="center">
  <img src="./assets/readme/free-demo.gif" alt="Agen AI memakai route Swobu dengan load balancing dan failover" width="1280">
</p>

[Dokumentasi](https://swobu.com/docs/) · [Mulai cepat](https://swobu.com/docs/start/first-route/) · [Rilis](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Agen dan klien yang didukung Swobu" width="900">
  <img src="./assets/readme/providers.png" alt="Provider yang didukung Swobu" width="1100">
</p>

---

## Agen memilih model. Swobu memilih tempat model itu dijalankan.

Bagi agen, sebuah **route Swobu terlihat seperti sebuah model**.

Di balik nama itu bisa ada satu endpoint, model yang sama dari beberapa tempat, atau pool lintas provider.

```text
claude-opus-5
    │
    ├─ Anthropic / claude-opus-5
    ├─ AWS Bedrock / account A / claude-opus-5
    └─ AWS Bedrock / account B / claude-opus-5
```

Tetap gunakan `claude-opus-5`. Swobu dapat membagi kapasitas dan melakukan failover di belakangnya.

Atau jadikan nama model menggambarkan sebuah pekerjaan:

```text
codex-auto-review
    │
    ├─ Deepseek / Deepseek V4 Flash
    ├─ Google / Gemini 3.7 Flash
    └─ another review model
```

Atau buat pool yang sengaja mencampur model dan provider:

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

Field `model` yang sudah dipahami agen Anda menjadi batas routing yang dapat diprogram.

---

## Mulai dengan satu perintah

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Installer menjalankan Swobu dan membuka **Cockpit**, antarmuka terminalnya.

Tambahkan provider, buat route, lalu hubungkan agen Anda.

### Hubungkan agen

Cockpit dapat mengonfigurasi klien yang didukung untuk Anda.

Atau gunakan CLI:

```bash
swobu connect claude
swobu connect codex
swobu connect openclaw
swobu connect pi
swobu connect kilo
swobu connect hermes
```

Setelah itu, agen berbicara dengan Swobu. Konfigurasi provider dan routing tetap berada di belakang gateway.

[Mulai cepat dalam 5 menit →](https://swobu.com/docs/start/first-route/)

---

## Apa yang berubah saat nama model menjadi route?

### Gabungkan kapasitas

Sebuah target bukan sekadar model.

Target dapat mewakili:

- provider
- akun
- region cloud
- hosted endpoint
- server lokal
- model

Taruh beberapa target di tier yang sama untuk membagi beban di antaranya.

Tambahkan fallback tier untuk menentukan apa yang terjadi ketika kapasitas utama tidak tersedia.

```text
route: gpt-5.6-sol

primary
├─ Azure / westcentralus / gpt-5.6-sol
└─ Azure / westus2 / gpt-5.6-sol

fallback
└─ OpenAI / gpt-5.6-sol
```

Agen tetap meminta `gpt-5.6-sol`.

---

### Routing lintas provider

Route tidak harus mempertahankan identitas model.

Nama seperti `review`, `cheap`, `free`, atau `codex-auto-review` dapat mewakili kapasitas apa pun yang cocok untuk pekerjaan tersebut.

```text
review
├─ Z.AI / GLM-5.3
├─ Kimi / Kimi-2.8
└─ Ollama / Qwen3-Coder
```

Dengan begitu, beberapa agen dapat berbagi kebijakan routing tanpa menanam konfigurasi provider ke setiap klien.

---

### Failover tanpa mengonfigurasi ulang agen

Kuota habis. Region tidak tersedia. Endpoint gagal. Akun mencapai batas.

Swobu dapat mencoba target berikutnya yang memenuhi syarat sesuai route.

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

Nama route tidak berubah.

---

### Intent reasoning ikut bersama request

Setiap provider mengekspos reasoning dengan cara berbeda.

Satu API mungkin menerima effort level. API lain mengekspos token budget. Yang lain lagi mengodekan reasoning dengan bentuk request yang sepenuhnya berbeda.

Swobu memperlakukan reasoning sebagai kemampuan semantik dan menerjemahkannya ketika ada representasi ekuivalen yang tetap bermakna.

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

Anda seharusnya tidak perlu mengajari setiap agen dialek API setiap provider.

---

### Kompatibilitas membuat routing benar-benar mungkin

Mengirim JSON yang sama ke URL lain itu mudah.

Memindahkan request agen dengan aman antar-API tidak mudah.

Provider berbeda dalam hal:

- tools dan function calls
- reasoning
- web search
- streaming
- message history
- structured content
- model discovery
- kemampuan native provider
- detail protokol dan edge case

Swobu menerjemahkan request ketika maknanya dapat dipertahankan.

Target yang tidak dapat merepresentasikan semantik yang dibutuhkan bisa dikeluarkan, alih-alih diam-diam menurunkan kualitas request.

Kompatibilitas bukan sesuatu yang seharusnya perlu Anda pikirkan terus. Itu adalah infrastruktur yang membuat routing dapat dipercaya.

---

## Satu boundary, banyak protokol

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

Saat ini Swobu mendukung integrasi provider di berbagai protokol, termasuk:

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages
- Gemini Interactions

Dukungan protokol dan kemampuan yang tepat berbeda menurut provider.

[Matriks kemampuan →](https://swobu.com/docs/)

---

## Provider

Swobu mendukung inferensi lokal, frontier API, hyperscaler, platform inferensi khusus, dan aggregator.

**Local:** Ollama · LM Studio · vLLM

**Frontier:** OpenAI · ChatGPT · Anthropic · Gemini · Mistral · DeepSeek · Kimi · StepFun · Z.AI

**Cloud:** AWS Bedrock · Azure AI · Cloudflare Workers AI · Scaleway · OVHcloud

**Inference:** Cerebras · Groq · SambaNova · NVIDIA NIM · Together AI · Fireworks AI · FriendliAI · DeepInfra · Runpod · Nebius · GMI Cloud · Novita AI · SiliconFlow · Baseten · Hyperbolic · ModelScope · LLM7

**Aggregation:** OpenRouter · Custom Endpoint

Katalog, jumlah provider, matriks protokol, dan aset README dihasilkan dari provider registry Swobu.

---

## Kemampuan native tetap native

Swobu tidak memaksa semua provider turun ke sekumpulan kemampuan minimum yang sama.

Ketika target yang dipilih menyediakan kemampuan native berguna yang dipahami Swobu, kemampuan itu tetap dapat tersedia melewati boundary kompatibilitas.

Termasuk kemampuan seperti web search native provider pada provider yang mendukungnya.

Prinsipnya sederhana:

> **pertahankan semantik yang berguna bila memungkinkan; gagal secara jelas bila tidak dapat direpresentasikan.**

---

## Dibangun terhadap inkompatibilitas nyata

Swobu ada karena “OpenAI-compatible” sering berhenti kompatibel tepat ketika agen mulai melakukan hal-hal yang menarik.

Swobu diuji terhadap kegagalan nyata yang melibatkan:

- reasoning controls
- tool definitions
- field malformed atau unsupported
- message replay
- model discovery
- perilaku streaming
- translasi lintas protokol
- pembatasan request khusus provider

[Catatan kompatibilitas →](https://swobu.com/docs/)

---

## Contoh

### Model yang sama, beberapa provider

Pertahankan nama model yang sudah digunakan agen sambil menambahkan kapasitas redundan di belakangnya.

### Pool gratis lintas provider

Gabungkan kapasitas gratis yang berulang di balik satu nama model.

### Lokal dulu, cloud bila diperlukan

Prioritaskan Ollama, LM Studio, atau vLLM lalu fallback ke kapasitas hosted sesuai kebijakan.

### Route khusus agen

Ekspos nama seperti `codex-auto-review` atau `claude-plan`, sementara provider dan model di belakangnya dapat diubah secara independen.

---

## Local-first

Swobu berjalan secara lokal dan mengekspos endpoint yang dihubungi agen Anda.

Credential provider tetap berada di gateway, bukan disalin ke setiap klien.

Tidak perlu akun Swobu untuk penggunaan lokal.

Telemetri operasional sengaja dibatasi dan dapat dinonaktifkan.

[Keamanan & privasi →](https://swobu.com/docs/)

---

## Rilis

Swobu menerbitkan binary berversi untuk Linux, macOS, dan Windows, lengkap dengan checksum SHA-256.

[Rilis terbaru →](https://github.com/swobuforge/swobu/releases/latest)

Instal dari source:

```bash
go install github.com/swobuforge/swobu/cmd/swobu@latest
```

---

## Status

Routing, perilaku kompatibilitas, dan integrasi provider terus berkembang sementara abstraksinya distabilkan.

Laporan bug dan masalah kompatibilitas sangat diterima.

[Buka issue →](https://github.com/swobuforge/swobu/issues)

---

<details>
<summary><strong>OpenAI Build Week 2026</strong></summary>
Arsitektur Swobu saat ini dibangun ulang selama OpenAI Build Week 2026 menggunakan GPT 5.6 Sol.
</details>

---

<p align="center">
  <strong>Satu nama model. Kapasitas apa pun di belakangnya.</strong>
</p>

<p align="center">
  <a href="https://swobu.com/docs/start/first-route/">Mulai</a>
  ·
  <a href="https://swobu.com/docs/">Docs</a>
  ·
  <a href="https://github.com/swobuforge/swobu/releases">Rilis</a>
</p>
