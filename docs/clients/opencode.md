# OpenCode

Use Cockpit's workspace `endpoint` disclosure or run `swobu connect opencode`.
Connect adds the `swobu` provider, selects `swobu/default`, and points OpenCode's
Responses client at the selected workspace endpoint. Use `--workspace <name>`
when workspace selection is ambiguous; replacing an owned value requires
`--replace`.

Swobu reads OpenCode's normal global configuration layers in their effective
order:

```text
~/.config/opencode/config.json
~/.config/opencode/opencode.json
~/.config/opencode/opencode.jsonc
```

`$XDG_CONFIG_HOME/opencode` replaces `~/.config/opencode` when
`XDG_CONFIG_HOME` is set. Connect writes only the highest-priority existing
file, or `opencode.jsonc` for a fresh configuration. It inspects only the
Swobu-owned model, provider package, endpoint, API-key presence, and model
presence needed to review that write. Comments and all other settings remain
operator-owned.

Connect configures the normal global default; invocation-specific environment,
project, organization, and managed settings may still override it. Connect
detects the V1 `provider` and V2 `providers` schema and updates the matching
Swobu model binding without changing V2's `websearch` provider, enablement, or
first-use consent. Conflicting V1 and V2 schemas fail planning rather than
guessing.

OpenCode V1 gates Exa/Parallel-backed `websearch` for non-OpenCode providers on
`OPENCODE_ENABLE_EXA` or `OPENCODE_ENABLE_PARALLEL`, read when OpenCode starts.
Connect does not change process or shell environment; configure the relevant
variable before launching OpenCode if that V1 search backend is selected.

`swobu/default` is a route facade, so Connect does not advertise context or
output limits when the selected route's concrete model limits are unavailable.
An existing Swobu model limit remains operator-owned and is not replaced or
validated.
