# Claude Code

1. Run `swobu` and open the workspace `endpoint` row in Cockpit.
2. Select `Claude Code`, review the endpoint change, and apply.
3. Run Claude Code normally. Cockpit Activity provides runtime traffic proof.

For headless use, run `swobu connect claude`. Add `--workspace <name>` when more
than one workspace exists, and `--replace` only when replacing a different
existing endpoint.

Connect changes only `env.ANTHROPIC_BASE_URL` in the Claude Code user
`settings.json`. It does not change model, context, auth, permissions, plugins,
hooks, or MCP settings.

The value is the same canonical unversioned workspace URL used by every Swobu
client, for example `http://127.0.0.1:7926/c/personal`. Claude Code appends its
Messages operation; Connect never writes a `/v1` base that could double it.

If request behavior differs by endpoint family (`/v1/messages` vs OpenAI-style families), verify the client mode and route are aligned.
