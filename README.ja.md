# Swobu

[English](README.md) · [简体中文](README.zh-CN.md) · **日本語** · [Português (Brasil)](README.pt-BR.md) · [Bahasa Indonesia](README.id.md) · [한국어](README.ko.md) · [Русский](README.ru.md) · [Español](README.es.md) · [Українська](README.uk.md)

**Claude Code、Codex、その他の AI エージェントを 1 つのエンドポイントから DeepSeek、Kimi、GLM、OpenAI、Anthropic、OpenRouter、Ollama、Bedrock などへルーティング。負荷分散とフェイルオーバーも自動化します。**

AI の計算資源をルーティング可能にします。エージェントはモデル名を指定するだけ。Swobu がその名前を、プロバイダー、アカウント、リージョン、ローカルサーバーをまたぐルートへ変換し、負荷分散、フェイルオーバー、reasoning の変換、意味を保つプロトコル互換性を裏側で処理します。

<p align="center">
  <img src="./assets/readme/free-demo.gif" alt="Swobu のルートで負荷分散とフェイルオーバーを行う AI エージェント" width="1280">
</p>

[ドキュメント](https://swobu.com/docs/) · [クイックスタート](https://swobu.com/docs/start/first-route/) · [リリース](https://github.com/swobuforge/swobu/releases)

<p align="center">
  <img src="./assets/readme/clients.png" alt="Swobu が対応するエージェントとクライアント" width="900">
  <img src="./assets/readme/providers.png" alt="Swobu が対応するプロバイダー" width="1100">
</p>

---

## エージェントがモデルを選び、Swobu が実行先を選ぶ

エージェントから見ると、Swobu の**ルートは 1 つのモデルのように見えます**。

その名前の裏側には、単一エンドポイント、複数の場所で利用できる同一モデル、あるいはプロバイダーをまたぐプールを置けます。

```text
claude-opus-5
    │
    ├─ Anthropic / claude-opus-5
    ├─ AWS Bedrock / account A / claude-opus-5
    └─ AWS Bedrock / account B / claude-opus-5
```

エージェントは引き続き `claude-opus-5` を使います。Swobu がその裏側で容量を分散し、必要ならフェイルオーバーします。

モデル名を用途そのものにすることもできます。

```text
codex-auto-review
    │
    ├─ Deepseek / Deepseek V4 Flash
    ├─ Google / Gemini 3.7 Flash
    └─ another review model
```

異なるモデルやプロバイダーを意図的に混ぜたプールも作れます。

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

エージェントがすでに理解している `model` フィールドが、そのままプログラム可能なルーティング境界になります。

---

## 1 コマンドで開始

```bash
curl -fsSL https://swobu.com/install.sh | sh
```

インストーラーが Swobu を起動し、ターミナル UI の **Cockpit** を開きます。

プロバイダーを追加し、ルートを作成して、エージェントを接続します。

### エージェントを接続する

Cockpit から対応クライアントを設定できます。

CLI を使うこともできます。

```bash
swobu connect claude
swobu connect codex
swobu connect openclaw
swobu connect pi
swobu connect kilo
swobu connect hermes
```

以後、エージェントは Swobu と通信します。プロバイダー設定とルーティングはゲートウェイの向こう側に残ります。

[5 分クイックスタート →](https://swobu.com/docs/start/first-route/)

---

## モデル名がルートになると何が変わるのか

### 容量をプールする

target は単なるモデルではありません。

たとえば次のものを表せます。

- プロバイダー
- アカウント
- クラウドリージョン
- ホストされたエンドポイント
- ローカルサーバー
- モデル

複数の target を同じ tier に置けば、その間で負荷分散できます。

fallback tier を追加すれば、優先容量が使えない場合の遷移先も定義できます。

```text
route: gpt-5.6-sol

primary
├─ Azure / westcentralus / gpt-5.6-sol
└─ Azure / westus2 / gpt-5.6-sol

fallback
└─ OpenAI / gpt-5.6-sol
```

エージェントは変わらず `gpt-5.6-sol` を要求します。

---

### プロバイダーをまたいでルーティングする

ルートはモデルの同一性を保つ必要がありません。

`review`、`cheap`、`free`、`codex-auto-review` のような名前に、その用途に合う任意の容量を割り当てられます。

```text
review
├─ Z.AI / GLM-5.3
├─ Kimi / Kimi-2.8
└─ Ollama / Qwen3-Coder
```

これにより、各エージェントへプロバイダー設定をハードコードせず、複数のエージェントで同じルーティングポリシーを共有できます。

---

### エージェントを再設定せずにフェイルオーバー

クォータ切れ。リージョン障害。エンドポイント障害。アカウントの制限到達。

Swobu はルートに従って、次に利用可能な target を試します。

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

ルート名は変わりません。

---

### reasoning の意図もリクエストと一緒に移動する

reasoning の表現方法はプロバイダーごとに違います。

ある API は effort level を受け取り、別の API は token budget を公開し、さらに別の API はまったく異なるリクエスト形状で reasoning を表します。

Swobu は reasoning を意味的な capability として扱い、意味のある対応表現が存在する場合に変換します。

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

プロバイダーを変えるたびに、すべてのエージェントへ別の API 方言を教える必要はありません。

---

### 互換性があるからルーティングできる

同じ JSON を別 URL に送るだけなら簡単です。

エージェントのリクエストを API 間で安全に移動させるのは簡単ではありません。

プロバイダー間では次の点が食い違います。

- tools / function calls
- reasoning
- web search
- streaming
- message history
- structured content
- model discovery
- プロバイダー固有 capability
- プロトコルの細部や edge case

意味を保てる場合、Swobu はリクエストを変換します。

必要な意味を表現できない target は、黙って劣化させるのではなく候補から外せます。

互換性は毎回考え続けるべき製品機能ではありません。信頼できるルーティングを成立させる基盤です。

---

## 1 つの境界、複数のプロトコル

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

Swobu は現在、以下を含む複数プロトコルのプロバイダー統合に対応しています。

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages
- Gemini Interactions

正確なプロトコルおよび capability 対応はプロバイダーごとに異なります。

[Capability matrix →](https://swobu.com/docs/)

---

## プロバイダー

Swobu はローカル推論、frontier API、ハイパースケーラー、特化型推論プラットフォーム、アグリゲーターに対応しています。

**Local:** Ollama · LM Studio · vLLM

**Frontier:** OpenAI · ChatGPT · Anthropic · Gemini · Mistral · DeepSeek · Kimi · StepFun · Z.AI

**Cloud:** AWS Bedrock · Azure AI · Cloudflare Workers AI · Scaleway · OVHcloud

**Inference:** Cerebras · Groq · SambaNova · NVIDIA NIM · Together AI · Fireworks AI · FriendliAI · DeepInfra · Runpod · Nebius · GMI Cloud · Novita AI · SiliconFlow · Baseten · Hyperbolic · ModelScope · LLM7

**Aggregation:** OpenRouter · Custom Endpoint

カタログ、プロバイダー数、プロトコルマトリクス、README のアセットは Swobu の provider registry から生成されます。

---

## ネイティブ capability はネイティブのまま

Swobu はすべてのプロバイダーを最小共通機能へ落としません。

選択された target が Swobu の理解できる有用なネイティブ capability を公開していれば、その capability を互換境界越しに維持できます。

対応プロバイダーの provider-native web search もその一例です。

原則は単純です。

> **有用な意味を保てるなら保つ。表現できないなら明確に失敗する。**

---

## 実際の非互換性を前提に構築

Swobu が存在する理由は、「OpenAI-compatible」がエージェントの処理が面白くなる地点で互換ではなくなることが多いからです。

実際の失敗例に対してテストしています。

- reasoning controls
- tool definitions
- malformed / unsupported fields
- message replay
- model discovery
- streaming behavior
- cross-protocol translation
- provider-specific request restrictions

[互換性ノート →](https://swobu.com/docs/)

---

## 例

### 同じモデル、複数プロバイダー

エージェントが使うモデル名はそのままに、裏側へ冗長な容量を追加します。

### プロバイダー横断の無料プール

継続的な無料枠を 1 つのモデル名の裏側へまとめます。

### ローカル優先、必要ならクラウド

Ollama、LM Studio、vLLM を優先し、ポリシーに従ってホスト型容量へフォールスルーします。

### エージェント別ルート

`codex-auto-review` や `claude-plan` のような名前を公開し、その裏側のプロバイダーやモデルだけを独立して変更できます。

---

## Local-first

Swobu はローカルで動き、エージェントが接続するエンドポイントを公開します。

プロバイダーの認証情報は各クライアントへコピーせず、ゲートウェイに保持します。

ローカル利用に Swobu アカウントは不要です。

運用テレメトリは意図的に最小化され、無効化もできます。

[セキュリティとプライバシー →](https://swobu.com/docs/)

---

## リリース

Swobu は Linux、macOS、Windows 向けのバージョン付きバイナリと SHA-256 チェックサムを公開します。

[最新リリース →](https://github.com/swobuforge/swobu/releases/latest)

ソースからインストール：

```bash
go install github.com/swobuforge/swobu/cmd/swobu@latest
```

---

## ステータス

抽象化を固めながら、ルーティング、互換動作、プロバイダー統合は現在も進化しています。

バグ報告、互換性レポートを歓迎します。

[Issue を開く →](https://github.com/swobuforge/swobu/issues)

---

<details>
<summary><strong>OpenAI Build Week 2026</strong></summary>
Swobu の現在のアーキテクチャは OpenAI Build Week 2026 中に GPT 5.6 Sol を使って再構築されました。
</details>

---

<p align="center">
  <strong>1 つのモデル名。その裏側に、どんな容量でも。</strong>
</p>

<p align="center">
  <a href="https://swobu.com/docs/start/first-route/">はじめる</a>
  ·
  <a href="https://swobu.com/docs/">ドキュメント</a>
  ·
  <a href="https://github.com/swobuforge/swobu/releases">リリース</a>
</p>
