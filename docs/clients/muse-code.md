# Muse Code

Swobu supports the named **Muse Code + Muse Spark 1.2** path through the OpenAI
Responses API.

1. Create a workspace whose default target uses **Meta Model API** with model
   `muse-spark-1.2`.
2. Open the Workspace Connect page in the Web Operator, select **Muse Code**, review,
   and apply.
3. Run Muse normally:

```bash
muse
```

For CLI preview:

```bash
swobu connect muse --dry-run --workspace coding --model coding --principal '<principal-id>'
```

Apply through the [reviewed Connect workflow](admin-mcp.md) with client `muse`,
an exact Workspace, private intent file and literal local confirmation. Then
run `muse` normally.

Connect points Muse at the workspace endpoint and selects Swobu's `default`
model. Its catalog advertises Swobu's conservative facade budget of 1,000,000
context tokens and 64,000 output tokens. The Meta credential stays in Swobu and
is not written to Muse.
