# vLLM backend

Start a generative vLLM inference server, then select vLLM in Cockpit. Swobu
uses `http://127.0.0.1:8000/v1` by default and targets vLLM 0.12.0 or later.
The standard serving contract was verified against vLLM 0.14.0 with model
`deepseek-ocr`, both without authentication and with `--api-key` Bearer
authentication.

vLLM supports Responses, Chat Completions, and Messages in buffered and
streaming forms. The preference-ordered provider manifest defaults to
Responses; operators may explicitly select any other advertised protocol.

Swobu discovers served model names through `GET /v1/models`. Catalog records
remain sparse and inherit protocol choices from the provider manifest; model
names never select tool, reasoning, protocol, or continuation behavior.

The credential is optional. When configured, Swobu sends it as an
`Authorization: Bearer` token on the `/v1` API calls Swobu makes. That does not
secure vLLM endpoints outside this API namespace.

Tool calling and reasoning depend on the served model and vLLM parser/server
configuration. Swobu uses its collision-safe portable tool-discovery projection
and preserves backend rejections as backend truth. Responses continuation is
retained only when the response confirms effective storage; no vLLM-specific
storage or continuation path exists.

Swobu does not expose vLLM extensions, operational APIs, lifecycle management,
embeddings, pooling, reranking, audio, or runtime LoRA administration.
