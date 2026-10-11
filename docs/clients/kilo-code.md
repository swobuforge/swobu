# Kilo Code

Use Cockpit's workspace `endpoint` disclosure or run `swobu connect kilo`.
Connect adds provider `swobu`, points it at the workspace endpoint, and selects
`swobu/default`. Existing unrelated Kilo Code settings remain owned by Kilo
Code.

Connect enables Kilo's documented `web_search` opt-in for non-Kilo providers so
an installed and configured Kilo search backend remains available. It does not
select a search vendor or change search credentials. If the existing config
explicitly sets `web_search: false`, Connect reports a replacement; review it
and pass `--replace` to enable the opt-in.

Use `--workspace <name>` when workspace selection is ambiguous. Replacing a
different client configuration requires `--replace`.
