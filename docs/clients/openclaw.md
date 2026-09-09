# OpenClaw

Use Cockpit's workspace `endpoint` disclosure or run `swobu connect openclaw`.
Connect uses OpenClaw's own validated configuration commands.

Connect adds provider `swobu`, points it at the workspace endpoint, and selects
`swobu/default`. Existing unrelated OpenClaw settings remain owned by OpenClaw.

Use `--workspace <name>` when workspace selection is ambiguous. Replacing
different client configuration requires `--replace`.
