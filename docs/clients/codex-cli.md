# Codex CLI

1. Run `swobu` and open the workspace `endpoint` row in Cockpit.
2. Select `Codex CLI`, review the Swobu backend declaration and selection, and apply.
3. Run Codex normally. Cockpit Activity provides runtime traffic proof.

For headless use, run `swobu connect codex`. Add `--workspace <name>` when more
than one workspace exists, and `--replace` only when replacing existing
different client configuration.

Connect selects Swobu as Codex's default model provider and points it at the
workspace endpoint. Existing unrelated Codex settings remain owned by Codex.
