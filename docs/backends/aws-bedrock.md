# AWS Bedrock backend

Configure AWS Bedrock Mantle in Swobu Cockpit by selecting a region and an API
URL. Region is selected and persisted explicitly; process region variables never
become route state. The API URL is the complete Bedrock base URL including its
AWS namespace (`/v1`, `/openai/v1`, or `/anthropic/v1`); it defaults from region
and may be edited. Swobu appends only the protocol operation
(`/responses`, `/chat/completions`, `/messages`).

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
  `/chat/completions`, and `/messages`, appended to the authored endpoint
- endpoint: the complete API base URL including its AWS namespace
  (`/v1`, `/openai/v1`, or `/anthropic/v1` as documented for the selected
  model); the host may be canonical AWS
  (`bedrock-mantle.<region>.api.aws`), AWS PrivateLink, or custom — all allowed,
  only the first two have a verified SigV4 contract
- model catalog: Mantle `/v1/models`, derived from the service root

If model loading or authentication fails, record:
- whether authentication was an explicit target credential or SigV4
- resolved region
- STS caller ARN or identity-probe error for SigV4
- request family
- backend error payload/status code
