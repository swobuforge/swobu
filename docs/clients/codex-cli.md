# Codex CLI

1. Run `swobu` and open the workspace `endpoint` row in Cockpit.
2. Select `Codex CLI`, review the Swobu backend declaration and selection, and apply.
3. Run Codex normally. Cockpit Activity provides runtime traffic proof.

For headless use, run `swobu connect codex`. Add `--workspace <name>` when more
than one workspace exists, and `--replace` only when replacing existing
different client configuration.

Connect selects `model_provider = "swobu"` and `model = "default"`, then creates
or reuses `[model_providers.swobu]` with the workspace `base_url` and Responses
wire protocol. A missing display name defaults to `Swobu`; an existing name or
other provider metadata is preserved. Connect does not change auth,
permissions, profiles, reasoning, MCP settings, or target-derived capabilities.

The value is Swobu's canonical unversioned workspace URL, for example
`http://127.0.0.1:7926/c/personal`. Codex sends its Responses operation beneath
that base; Swobu accepts both bare and `/v1`-prefixed operation spellings.

If routing appears correct but responses fail, capture request family and backend error payload for compatibility debugging.
