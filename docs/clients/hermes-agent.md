# Hermes Agent

Use Cockpit's workspace `endpoint` disclosure or run `swobu connect hermes`.

Connect selects Hermes' documented custom OpenAI-compatible main backend:
`model.provider = custom`, `model.default = default`, and `model.base_url` set
to the canonical workspace URL. Those three leaves are applied as one atomic,
source-preserving edit because partial main-model states are not harmless.
Unrelated YAML comments, formatting, credentials, fallback providers, and
auxiliary provider overrides remain Hermes-owned.

Automatic editing is intentionally limited to one block-style `model` mapping
with unique, single-line plain or quoted owned scalar values. Flow mappings,
block scalars, and duplicate `model` or owned keys fail closed without changing
the file.

Use `--workspace <name>` when workspace selection is ambiguous. Replacing
different owned main-model configuration requires `--replace`.
