# Z.AI Reasoning-Effort Live Contract

This opt-in probe implements the one-time matrix in
`docs/00-inbox/RFC-zai-reasoning-effort-static-empirical-contract.md`.

It is intentionally outside production packages and routine test entrypoints.
It writes only sanitized acceptance metadata: no credentials, prompts, model
output, reasoning content, authorization headers, or raw provider bodies.

Run from `swobucli/opencore`:

```sh
ZAI_API_KEY=... python3 scripts/zai-live-contract/reasoning_effort_probe.py \
  --output testdata/live-contracts/zai/reasoning-effort-2026-08-03.json
```

Before any paid run:

1. Run the exact command with `--dry-run`.
2. Record the printed `planned_generation_rows` and the retry/time bounds.
3. Obtain explicit approval for that bound.
4. Remove `--dry-run` without changing any matrix selector.

Never expand a live matrix while it is running. Treat a larger follow-up as a
new paid run requiring a new dry-run count and approval.

The probe writes a sibling JSONL journal before the first generation request,
then appends and `fsync`s every completed row. The JSON artifact is atomically
refreshed after each row. Re-running the same command resumes from the journal
and skips persisted rows.

The probe performs two independent passes by default and retries only
inconclusive transport, entitlement, rate-limit, or provider-failure outcomes.
Parameter errors remain binding rejections and are never retried as transient.

SIGINT and SIGTERM preserve completed rows and mark the artifact interrupted.
Do not run a paid live matrix with a probe that lacks this checkpoint contract.
