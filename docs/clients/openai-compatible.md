# OpenAI-compatible clients

Typical local client settings:

```sh
OPENAI_BASE_URL=http://127.0.0.1:7926/v1
OPENAI_API_KEY=swobu
```

Exact variable names depend on the client. Swobu handles backend auth and routing behind this local boundary.

## Responses WebSocket

Swobu accepts Responses `response.create` messages over the existing Workspace
Responses WebSocket endpoint. An optional `stream_id` selects a named lane:
requests in one lane run in order, while different lanes may execute concurrently.
Each response event and request-scoped error returns the matching `stream_id`.
Omit it for the original single default lane. Connect at
`/workspaces/{slug}/v1/responses`; creates stream implicitly without an HTTP
`stream` flag. `stream_id` accepts 1–256 ASCII letters, digits, underscores,
hyphens or periods. The distinct-name limit lasts for the socket lifetime.

```json
{"type":"response.create","stream_id":"planner","model":"your-model","input":"Hello"}
```

The transport admits up to 16 in-flight requests, 32 distinct named lanes, and
64 queued creates per socket. These are bounded socket scheduling limits, not
Workspace admission policy: every execution still passes current credential,
Models rate, concurrency and spending admission. Disconnect cancels all lanes.

Supported client message: `response.create`. `response.steer` and
`generate:false` warmup explicitly return an error without starting inference.
This transport **does not** create a local previous-response cache or promise
that a selected provider can resume `previous_response_id`. Clients needing
continuation must retain/replay the requisite context or use a backend that
supports it. This is transport multiplexing, not a conversation/session service.
