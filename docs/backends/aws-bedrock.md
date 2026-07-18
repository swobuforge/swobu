# AWS Bedrock backend

Configure AWS Bedrock Mantle in Swobu Cockpit by selecting a region. Region is
selected and persisted explicitly; process region variables never become route
state.

Authentication precedence is:

1. a configured `env:`, `file:`, or `secret:` credential reference is
   resolved and sent as a Bedrock bearer token;
2. the AWS SDK default credential chain when no target credential is configured;
   requests are then signed with SigV4.

Swobu does not persist or select AWS profiles, SSO sessions, access keys, or
other AWS credential-chain inputs. Cockpit probes STS caller identity for the
SigV4 path only as diagnostic enrichment; successful catalog access remains
usable when STS identity lookup fails. Refresh/retry reloads AWS-owned shared
configuration. The optional target-credential field composes the shared
environment, file-browser, and paste-token flows.

Current adapter scope is explicit:
- execution: Bedrock Mantle OpenAI-compatible requests on `/responses`,
  `/chat/completions`, and `/messages`
- endpoint host requirement: `bedrock-mantle.<region>.api.aws`
- model catalog: Mantle `/models`

If model loading or authentication fails, record:
- whether authentication was an explicit target credential or SigV4
- resolved region
- STS caller ARN or identity-probe error for SigV4
- request family
- backend error payload/status code
