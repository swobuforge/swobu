# Codex CLI

1. Run `swobu` and open the Workspace Connect page in the Web Operator.
2. Select `Codex CLI`, review the Swobu backend declaration and selection, and apply.
3. Run Codex normally. Web Operator Activity provides runtime traffic proof.

For CLI use, follow the [reviewed Connect workflow](admin-mcp.md): select an exact
Workspace, preview with `--dry-run`, then use `--apply --intent-file` and literal
local confirmation. Replacement is part of that review, not a `--replace` bypass.

Connect selects Swobu as Codex's default model provider and points it at the
workspace endpoint. Existing unrelated Codex settings remain owned by Codex.
