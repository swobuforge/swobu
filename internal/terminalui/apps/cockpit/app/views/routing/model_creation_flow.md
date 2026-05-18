# Model Creation Flow Grammar

Top-level owner: `BuildSection` in `section.go`.
Canonical state evaluator owner: `EvaluateCreateDraftRouteSetup` in
`app/state/model/create_route_setup_flow.go`.

Canonical semantic slot order for model creation and edit surfaces:

1. `provider`
2. `credential`
3. `scope` (only when provider requires one)
4. `credential dependency actions` (only when a pre-model dependency must be completed, for example `sign in`)
5. `model`
6. `delivery`

Rules:

- all providers use the same slot grammar; providers only fill values/capabilities
- model blocker hints render under `model` only
- model must not render before unresolved credential dependency actions
  (for example `sign in`)
- do not render `auth not required`; use `external` when Swobu is not the credential authority
- delivery is user-language first (`auto`, `streaming`, `non-streaming`)
- create readiness must derive from the same evaluator terminal state (`Ready`)
  used by routing rows; do not add provider-specific bypass checks in separate
  readiness paths

## Row Action Law

- every visible row must be either actionable (`choose`/`edit`/`change`) or
  explicitly informational with no action label
- do not render "choose ↵" on a row if no edit action exists for that row state
- blocker hints belong to the blocked row only (for example model blockers render
  under `model`, never under `delivery`)

## Bedrock Credential And Catalog Law

- Bedrock supports two credential strategies in this flow:
  - `aws_profile` (default chain/profile; external authority)
  - `env:AWS_BEARER_TOKEN_BEDROCK` (bearer token env reference)
- model catalog source depends on credential strategy:
  - `aws_profile` -> AWS SDK Bedrock `ListFoundationModels`
  - `env:*` -> OpenAI-compatible `/models` endpoint with bearer auth
- validation path must follow the same split; never probe `/models` for
  `aws_profile` mode.
- Bedrock region defaults are sourced from bundled opencore list assets under
  `internal/terminalui/apps/cockpit/app/views/routing/data/bedrock_regions.json`.
