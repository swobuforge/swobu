# LM Studio backend

Start the LM Studio local server, then select LM Studio in Cockpit. Swobu uses
`http://127.0.0.1:1234/v1` by default. First-class support targets LM Studio
0.4.1 and later. This support band remains experimental until the live smoke
lane records native discovery and one request through each protocol family.
Configured execution base URLs must end in `/v1`; discovery rejects any base
that would route inference outside LM Studio's compatibility namespace.

LM Studio supports Responses, Chat Completions, and Messages in buffered and
streaming forms. The credential is optional. When configured, Swobu sends it as
an `Authorization: Bearer` token.

Model discovery uses LM Studio's native `GET /api/v1/models` catalog so the
model key, display name, publisher, and architecture remain available to the
operator. Embedding models are excluded because Swobu routes generative
inference. A 404 or 405 from the native route retries the OpenAI-compatible
`GET /v1/models` catalog; authentication, rate-limit, server, transport, and
decode failures do not trigger that fallback.

Inference continues to use `/v1/responses`, `/v1/chat/completions`, and
`/v1/messages`. Swobu does not use native chat or LM Studio model
load/unload/download operations. Remote MCP is resolved and executed by
Exchange, and LM Studio receives ordinary callable functions. Model metadata
never selects request-path behavior.
