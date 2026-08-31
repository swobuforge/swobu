# Meta Model API — Muse Spark 1.2

The first-class **Meta Model API** profile is the supported backend for **Muse
Spark** and the tested **Muse Spark 1.2** integration.

Configure a target in Cockpit with:

- provider: `Meta Model API`
- model: `muse-spark-1.2`
- credential: `MODEL_API_KEY` or an equivalent Swobu credential reference
- protocol: derived `responses_stream`

The fixed provider base is `https://api.meta.ai/v1`. Swobu uses advisory
`GET /models` discovery and sends streaming Responses requests to
`POST /responses` with Bearer authentication. The profile intentionally exposes
no Chat Completions, Messages, OAuth, device login, or hardcoded provider-wide
model catalog.

For Muse Code setup, see [Muse Code](../clients/muse-code.md). The client sees
only Swobu's facade model `default`; backend model identity and credentials stay
behind the workspace route.
