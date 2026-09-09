# AWS Bedrock backend

Configure AWS Bedrock Mantle in Swobu Cockpit by selecting a region and an API
URL. Use the complete Bedrock base URL shown by AWS for the selected model,
including its `/v1`, `/openai/v1`, or `/anthropic/v1` namespace.

Authentication supports either:

1. a configured `env:`, `file:`, or `secret:` bearer-token reference; or
2. the AWS SDK default credential chain with SigV4.

Swobu does not persist or select AWS profiles, SSO sessions, access keys, or
other AWS credential-chain inputs.
