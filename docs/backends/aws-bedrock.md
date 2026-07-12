# AWS Bedrock backend

Configure AWS Bedrock Mantle in Swobu cockpit, including credential strategy
and region.

Current adapter scope is explicit:
- execution: Bedrock Mantle OpenAI-compatible requests on `/responses`,
  `/chat/completions`, and `/messages`
- endpoint host requirement: `bedrock-mantle.<region>.api.aws`
- model catalog: Mantle `/models`

If model loading or auth validation fails, record:
- selected credential mode
- resolved region
- request family
- backend error payload/status code
