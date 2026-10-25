# Workspace Estimated Spend

Workspace limits protect aggregate estimated USD spend across credentials.
Their desired policy lives under `accounting.limits` in `swobu.yaml`,
while `accounting.json` holds only crash-sensitive reservation/settlement evidence.
A limit can be removed with a revision-checked DELETE against the local
`/api/v1/workspaces/{slug}/limits` endpoint; removing policy does not delete
unsettled reservations or rewrite historical estimates.
They are soft thresholds, not a guarantee about provider invoices or maximum
charges. Accounting is synchronous and independent of the optional Ledger.

Local Node Control exposes `GET /api/v1/workspaces/{slug}/limits/status`.
It reports the current UTC window, configured limit, reserved/settled/uncertain
and aggregate charged cents, remaining capacity, and counts of unknown-price,
partial-quantity and unquoted operations. Totals are exact decimal strings;
they must not be parsed as floating-point financial values. This read is
no-store and rejects Workspace credentials. Reading it does not reset a budget
or advance the durable accounting clock. Settled cents are observed estimated
costs, not confirmed invoice amounts; uncertain cents are conservative holds.

Inspect from the local operator CLI:

```sh
swobu workspace limits --workspace coding
swobu workspace limits --workspace coding --json
```

Human output keeps exact USD cents, the UTC period, enforcement mode and
unknown/partial/unquoted coverage visible. JSON preserves the typed Control
response and decimal-string totals for automation. No interactive confirmation
is required for this read; it never changes a limit, rotates a credential or
contacts a provider. Use `--addr host:port` to select the local Node.

For an MCP client, mount `swobu-admin` in a normal Workspace and select
`workspace_limits_status`. Call it with `{"workspace":"coding"}` through the
Workspace MCP endpoint and ordinary admitted credential. Accounting totals remain
exact decimal strings with their UTC window and coverage gaps. Reviewed policy
changes use the same canonical command owner as Web Operator; queries do not
contact providers. See [Admin workflows](clients/admin-mcp.md).

Without a configured price source, enforcing limits reject unpriced provider
attempts with `COST_UNAVAILABLE`. Observe-only can admit unknown pricing without
claiming coverage.

Set `SWOBU_PRICING_CONFIG` to an absolute JSON file path to select prices from a
local accepted Models SQLite catalogue. Example:

```json
{
  "models_database": "/absolute/path/models.sqlite",
  "bindings": [
    {
      "target_id": "primary",
      "target_version": 1,
      "provider": "openai",
      "model": "exact-provider-selector",
      "base_url": "https://api.openai.com/v1",
      "external_version": "",
      "tariff": "standard",
      "input_forecast_tokens": 10000,
      "output_forecast_tokens": 10000
    }
  ]
}
```

All identity fields must match the selected target; a changed generation or
endpoint does not silently inherit the old price binding. Tariff names are
provider-local, not a global service tier. Settings contain no credentials.
Invalid settings fail startup; missing selectors or unavailable prices remain
unknown. The reader never modifies or creates the Models catalogue.

Forecasts are explicit positive operator estimates, applied to each provider
attempt, including tool-loop rounds. They are not measured token counts or
maximum output bounds. Every quote records `quantity_coverage: partial`.
Choose forecasts appropriate to the workload; inaccurate estimates may cause
early denial or overshoot. Restart reloads settings but preserves already
admitted quotes and charges. Credential rotation does not reset spend.

Current quoting covers ordinary USD input/output only. Cache/reasoning and
other billable dimensions are not covered by the admission forecast. Terminal
usage can settle frozen rates only with complete internally consistent usage
dimensions and an unchanged input-band quantity. Otherwise the admitted charge
remains uncertain. Non-USD conversion, media/tool costs, live provider pricing
acquisition, automatic token forecasting, public pricing distribution, and
invoice reconciliation remain unqualified. Do not treat this setting as a
complete hard budget for those workloads.
