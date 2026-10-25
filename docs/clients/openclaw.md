# OpenClaw

Use the Workspace Connect page in the Web Operator or the
[reviewed Connect workflow](admin-mcp.md) with client `openclaw`.
Connect uses OpenClaw's own validated configuration commands.

Connect adds provider `swobu`, points it at the workspace endpoint, and selects
`swobu/coding`. Existing unrelated OpenClaw settings remain owned by OpenClaw.

CLI apply requires an exact Workspace, private intent file and literal scoped
confirmation, including replacement of different client configuration.

The `coding` ID in these examples is not reserved: Connect uses the concrete model ID selected for that Workspace, which must match one of its ordered route patterns.
