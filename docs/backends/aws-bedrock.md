# AWS Bedrock backend

Configure AWS Bedrock in Swobu cockpit, including credential strategy and region.

Current adapter scope is explicit:
- execution: native Bedrock runtime operations (`Converse` for conversational canonicals and `InvokeModel` for prompt canonicals)
- endpoint host requirement: `bedrock-runtime.<region>...` or `bedrock-mantle.<region>...`
- model catalog: Bedrock control-plane `/foundation-models`

If model loading or auth validation fails, record:
- selected credential mode
- resolved region
- request family
- backend error payload/status code
