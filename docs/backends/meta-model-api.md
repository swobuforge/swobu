# Meta Model API

Select **Meta Model API** in Cockpit to route requests to Muse Spark.

Configure a target in Cockpit with:

- provider: `Meta Model API`
- model: a model available to your Meta account
- credential: `MODEL_API_KEY` or an equivalent Swobu credential reference
- protocol: streaming Responses (`responses_stream`)

The fixed provider base is `https://api.meta.ai/v1`. Swobu uses advisory
`GET /models` discovery and sends streaming Responses requests to
`POST /responses` with Bearer authentication.

For Muse Code setup, see [Muse Code](../clients/muse-code.md). The client sees
only Swobu's facade model `default`; backend model identity and credentials stay
behind the workspace route.
