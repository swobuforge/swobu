# Claude Code

1. Run `swobu` and open the workspace `endpoint` row in Cockpit.
2. Select `Claude Code`, review the endpoint change, and apply.
3. Run Claude Code normally. Cockpit Activity provides runtime traffic proof.

For headless use, run `swobu connect claude`. Add `--workspace <name>` when more
than one workspace exists, and `--replace` only when replacing a different
existing endpoint.

Connect points Claude Code at the workspace endpoint and enables workspace
model discovery. Existing unrelated Claude Code settings remain owned by
Claude Code.
