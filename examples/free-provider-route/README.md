# Credential-Proven Free Provider Route

This demo is deliberately conservative. A provider/model enters the route only
after a successful credentialed Swobu product E2E recorded the exact selector
or model. Provider pricing pages and model catalogs do not qualify a target.

Copy the fixture, replace the Workers AI account placeholder, export the listed
credentials, and start Swobu:

```sh
cp examples/free-provider-route/swobu.yaml /tmp/swobu-free.yaml
sed -i 's/REPLACE_WITH_ACCOUNT_ID/your-cloudflare-account-id/' /tmp/swobu-free.yaml
swobu --config /tmp/swobu-free.yaml
```

The endpoint is `http://127.0.0.1:7926/c/free-demo`. Clients may request the
route name `free`, `default`, or another non-empty fixed model name; the latter
two resolve through `default_route`.

## Token-effective summary

**Best overall recurring-free pair:** Cloudflare Workers AI with
`@cf/google/gemma-4-26b-a4b-it`. It has the strongest current Swobu proof among
the recurring-free targets: a streamed function call, tool result, continuation,
and final answer. Cloudflare resets its Free-plan allowance daily, but meters it
in Neurons rather than portable input/output tokens.

Choose a different first target when one constraint matters more:

| Need | Provider + target | Why | Material limit |
| --- | --- | --- | --- |
| Most published recurring token capacity | LLM7 + `default` | Published allowance is 1M tokens per 24 hours and 100 requests/hour | Dynamic model; current Swobu E2E proves text streaming, not free-tier tools |
| Fast agent reasoning | Groq + `openai/gpt-oss-20b` | Credentialed reasoning and tool continuation pass at Groq speed | Published free allowance is 200K tokens/day despite a high request ceiling |
| Strongest proven recurring tool loop | Workers AI + Gemma 4 | Two-call tool continuation passes on the Free plan | 10K Neurons/day is not directly convertible to tokens |
| Automatic free-model selection | OpenRouter + `openrouter/free` | Router selects a currently available free model; one observed run selected `openai/gpt-oss-20b:free` | About 50 free requests/day; model identity and capability can change per request |
| Extra prototype capacity | NVIDIA NIM Hosted + `nvidia/nemotron-mini-4b-instruct` | Credentialed hosted streaming passes | Free hosted access is for prototyping; no stable aggregate token allowance is asserted here |
| Account-specific monthly battery | Mistral + `ministral-3b-2512` | Direct Studio key, catalog, and streaming Chat pass | Free Mode allowance and available models are controlled by the account |
| Exhaust last, not recurring | Cerebras + `gemma-4-31b` | Credentialed reasoning/tool continuation passes | Signup credit only; remove the tier when credit is exhausted |

These allowance figures come from the lead research supplied on 2026-08-19;
the Swobu E2Es verify request-path behavior, not billing dashboards or quota
size. “Token-effective” therefore means useful completed agent work per stated
allowance, not a mathematically comparable token total.

## Recurring free vs free-model-only

- **Recurring free capacity:** Workers AI, Groq, LLM7, OpenRouter, and Mistral
  Free Mode reset or replenish an allowance on a daily or monthly cycle.
- **Recurring access with an unstable/unpublished aggregate allowance:** NVIDIA
  NIM Hosted exposes free prototype endpoints, but this recipe does not assert a
  durable token total.
- **Free model/selector only:** `openrouter/free` and LLM7 `default` describe
  dynamic routing to free models. The selector being free does not guarantee a
  fixed model, tools, context window, or large recurring quota. Mistral's exact
  model is likewise usable only when the account's Free Mode includes it.
- **One-time free credit:** Cerebras is not a recurring tier. It is free only
  while signup credit remains.
- **Not currently qualified:** Z.AI advertises `glm-4.7-flash` at $0, but two
  credentialed attempts returned provider 429/1305. A $0 model price without a
  successful usable request is not route capacity.

## Included evidence

| Target ID | Provider | Exact target | Credential | Successful live evidence |
| --- | --- | --- | --- | --- |
| `nvidia-free` | NVIDIA NIM Hosted | `nvidia/nemotron-mini-4b-instruct` | `NVIDIA_API_KEY` | `TestNVIDIAProductWedge`, 2026-08-18 |
| `workers-ai-free` | Cloudflare Workers AI | `@cf/google/gemma-4-26b-a4b-it` | `CLOUDFLARE_API_TOKEN` | `TestWorkersAIProductWedge`, 2026-08-18 |
| `openrouter-free` | OpenRouter | dynamic `openrouter/free` selector | `OPENROUTER_API_KEY` | `TestFreeCapacityTargets`, 2026-08-19 |
| `groq-free` | Groq | `openai/gpt-oss-20b` | `GROQ_API_KEY` | `TestGroqProductWedge`, 2026-08-19 |
| `mistral-free` | Mistral | `ministral-3b-2512` | `MISTRAL_API_KEY` | `TestMistralProductWedge`, 2026-08-19 |
| `llm7-free` | LLM7 | dynamic `default` selector | `LLM7_API_KEY` | `TestLLM7ProductWedge`, 2026-08-19 |
| `cerebras-free-credit` | Cerebras signup credit | `gemma-4-31b` | `CEREBRAS_API_KEY` | `TestCerebrasProductWedge`, 2026-08-18 |

All targets are grouped into a single primary tier and balanced across requests.
This is not a promise of permanent free access: the evidence dates are the freshness boundary.
Re-record the named scenario before changing a target or presenting its access as current.

## Excluded from this demo

- Gemini: explicitly excluded because it is unavailable for this recipe.
- Z.AI: two credentialed General API attempts with `glm-4.7-flash` on
  2026-08-19 returned provider HTTP 429/code 1305, so no successful free-model
  E2E exists yet. It remains excluded until that exact target passes.
- OVHcloud: anonymous live tool-loop characterization exists, but no
  credentialed product E2E meets this demo's admission rule.
- ModelScope and SambaNova: current ledgers are entitlement/credit gated rather
  than successful inference proof.
- Alibaba Model Studio: no native Swobu provider exists.
- GMI Cloud and SiliconFlow: no successful credentialed product E2E for the
  proposed free targets.

Cerebras is last because its evidence is signup credit, not a recurring free
tier. Remove that tier if the account has no remaining credit.
