# pi

Use the Workspace Connect page in the Web Operator or the
[reviewed Connect workflow](admin-mcp.md) with client `pi`.
Swobu uses Pi's global `~/.pi/agent/settings.json` and `models.json` (or the
documented `PI_CODING_AGENT_DIR` equivalents).

Connect owns the `swobu` provider binding. It writes the canonical unversioned
workspace URL, uses Pi's `openai-responses` API, and selects exactly one
client-visible model: the concrete model ID selected in the reviewed request.
It must match a saved Workspace route; no implicit `default` alias is installed.

Connect does not advertise reasoning, modality, context-window, or output-limit
claims for the facade. Pi applies its own defaults for omitted capability
fields. Richer capability propagation requires real route data and is outside
the connector.

Connect preserves a non-empty user API key, headers, `authHeader`, provider
display metadata, unrelated provider entries, and unrelated Pi configuration.
It normalizes only the Swobu provider's endpoint, protocol, and custom model
catalog. Provider `compat`, `modelOverrides`, and other Pi customizations are
preserved. Conflicting Swobu-owned configuration is disclosed in the original
review and requires literal scoped confirmation before CLI apply. Pi
`models.json` comments and trailing commas are accepted and unrelated source is
preserved.

Connect re-reads the files immediately before Apply, rejects changed reviewed
evidence, replaces each file atomically, and verifies convergence. It does not
copy Pi's private settings-lock implementation or claim a two-file transaction.

Select both `--workspace <name>` and `--model <model-id>` explicitly.
