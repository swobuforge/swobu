# AWS Bedrock backend

Configure AWS Bedrock in Swobu cockpit, including credential strategy and region.

If model loading or auth validation fails, record:
- selected credential mode
- resolved region
- request family
- backend error payload/status code
