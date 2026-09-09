# Muse Code

Swobu supports the named **Muse Code + Muse Spark 1.2** path through the OpenAI
Responses API.

1. Create a workspace whose default target uses **Meta Model API** with model
   `muse-spark-1.2`.
2. Open the workspace `endpoint` row in Cockpit, select **Muse Code**, review,
   and apply.
3. Run Muse normally:

```bash
muse
```

For headless setup:

```bash
swobu connect muse
muse
```

Connect points Muse at the workspace endpoint and selects Swobu's `default`
model. Its catalog advertises Swobu's conservative facade budget of 1,000,000
context tokens and 64,000 output tokens. The Meta credential stays in Swobu and
is not written to Muse.
