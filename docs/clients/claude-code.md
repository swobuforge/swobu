# Claude Code

1. Run `swobu` and open the workspace `endpoint` row in Cockpit.
2. Select `Claude Code`, review the endpoint change, and apply.
3. Run Claude Code normally. Cockpit Activity provides runtime traffic proof.

For headless use, run `swobu connect claude`. Add `--workspace <name>` when more
than one workspace exists, and `--replace` only when replacing a different
existing endpoint.

Connect changes `env.ANTHROPIC_BASE_URL` and sets
`env.CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY` to `"1"` in the Claude Code
user `settings.json`. It does not change model, context, auth, permissions,
plugins, hooks, or MCP settings. An existing `"0"` is an explicit choice, so
changing it uses the same `--replace` safeguard as replacing an endpoint.

The value is the same canonical unversioned workspace URL used by every Swobu
client, for example `http://127.0.0.1:7926/c/personal`. Claude Code appends its
Messages operation; Connect never writes a `/v1` base that could double it.

With discovery enabled, Claude Code requests `/v1/models` and presents the
workspace's `default` route followed by its named routes in lexical order.

If request behavior differs by endpoint family (`/v1/messages` vs OpenAI-style families), verify the client mode and route are aligned.
