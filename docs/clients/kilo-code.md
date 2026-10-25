# Kilo Code

Use the Workspace Connect page in the Web Operator or the
[reviewed Connect workflow](admin-mcp.md) with client `kilo`.
Connect adds provider `swobu`, points it at the workspace endpoint, and selects
`swobu/coding`. Existing unrelated Kilo Code settings remain owned by Kilo
Code.

Connect enables Kilo's documented `web_search` opt-in for non-Kilo providers so
an installed and configured Kilo search backend remains available. It does not
select a search vendor or change search credentials. If the existing config
explicitly sets `web_search: false`, Connect reports a replacement; review it
and confirm that exact original intent to enable the opt-in.

CLI apply requires an exact Workspace, private intent file and literal scoped
confirmation, including replacement of different client configuration.

The `coding` ID in these examples is not reserved: Connect uses the concrete model ID selected for that Workspace, which must match one of its ordered route patterns.
