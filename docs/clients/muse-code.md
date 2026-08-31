# Muse Code

Swobu supports the named **Muse Code + Muse Spark 1.2** path through the OpenAI
Responses API.

1. Create a workspace whose default target uses **Meta Model API** with model
   `muse-spark-1.2`.
2. Open the workspace `endpoint` row in Cockpit, select **Muse Code**, review,
   and apply.
3. Run Muse normally:

```bash
muse
```

For headless setup:

```bash
swobu connect muse
muse
```

Connect writes Muse's `provider = "meta"` profile discriminator, selects the
client-facing model `default`, points `endpoint_transport.base_url` at the
workspace `/v1` base, disables client-side auth, and replaces `model_catalog`
with one Swobu facade row. It preserves unrelated MCP, skill, hook, TUI, and
future settings. Existing owned values require explicit `--replace` approval.

Muse never receives `MODEL_API_KEY`. Swobu resolves that credential only when
calling Meta. This integration does not claim Muse compatibility with arbitrary
models or providers and does not add `/muse-code/models`.
