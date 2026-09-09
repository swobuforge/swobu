# [Swobu](https://swobu.com/)

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Português (Brasil)](README.pt-BR.md) · **Bahasa Indonesia** · [한국어](README.ko.md) · [Русский](README.ru.md) · [Español](README.es.md) · [Українська](README.uk.md)

**Satu endpoint untuk Claude Code, Codex, dan agen AI lain agar dapat memakai DeepSeek, Kimi, GLM, OpenAI, Anthropic, OpenRouter, Ollama, Bedrock, dan lainnya — dengan routing, load balancing, dan failover otomatis.**

Jadikan kapasitas AI dapat dirutekan. Agen Anda meminta sebuah model; Swobu mengubah nama model itu menjadi route lintas provider, akun, region, dan server lokal, sambil menangani load balancing, failover, translasi reasoning, serta kompatibilitas protokol secara semantik di belakang layar.

[Dokumentasi](https://swobu.com/docs/) · [Mulai cepat](https://swobu.com/docs/start/first-route/) · [Rilis](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Agen dan klien yang didukung Swobu" width="900">
  <img src="./assets/readme/providers.png" alt="Provider yang didukung Swobu" width="1100">
</p>

---

## Agen memilih model. Swobu memilih tempat model itu dijalankan.

Bagi agen, sebuah **route Swobu terlihat seperti sebuah model**.

Di balik nama itu bisa ada satu endpoint, model yang sama dari beberapa tempat, atau pool lintas provider.

Diagram berikut adalah contoh konfigurasi. Pilih model yang tersedia di provider Anda.

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

macOS, Linux, atau WSL:

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://swobu.com/install.ps1 | iex
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

[Lihat provider dan panduan konfigurasinya di dokumentasi.](https://swobu.com/docs/)

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

Build dari source:

```bash
git clone https://github.com/swobuforge/swobu.git
cd swobu
make build
./.out/swobu --version
```

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
