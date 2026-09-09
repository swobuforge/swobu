# Hermes Agent

Use Cockpit's workspace `endpoint` disclosure or run `swobu connect hermes`.

Connect selects Hermes' custom OpenAI-compatible backend, points it at the
workspace endpoint, and chooses Swobu's `default` model. Existing unrelated
Hermes settings remain owned by Hermes.

Use `--workspace <name>` when workspace selection is ambiguous and `--replace`
when replacing different main-model configuration.
