// Package target_config owns the mounted component for creating and editing
// route targets.
//
// TargetConfig is mounted by the routes section when the operator activates
// "add target" on an expanded route. It is not a global modal; it renders
// inline under the route row. Provider setup, model catalog probing, and
// routing choices backed by placement ranks are all internal to this package.
// Once a provider is selected, provider-specific GSX components render the
// visible row sequence below provider selection. The route section only knows
// whether a target config is open for a given route. Empty
// routes skip the routing chooser and create their first target at step 1.
// Provider forms author one locator value whose meaning comes from profile
// facts. One pure connection projection builds the routing.Connection used by
// catalog probing. The Cockpit adapter decodes opaque probe diagnostics before
// this feature receives typed Bedrock authentication evidence. Catalog success
// controls creation validity while optional STS identity enriches the Bedrock
// form. Every catalog-backed edit enters catalog-loading state before the
// component is mounted; BindApp resumes that pending operation while persisted
// model/protocol values remain non-authoritative selection seeds until
// reconciliation. Custom Endpoint retains its open-set authored model without
// requiring discovery. Bedrock has
// one authentication field: an absent credential reference
// selects AWS identity, while a present reference selects a bearer API key and
// embeds the shared credential chooser. ChatGPT retains its genuine
// login-session workflow. A mounted pending ChatGPT browser session is observed
// through the local daemon until it succeeds or fails; manual refresh remains a
// recovery action, and the observer ends with the form, mount, app, or session.
// Custom Endpoint treats catalog discovery as best-effort and uses the shared
// open-set model picker so an operator-authored model ID remains sufficient.
// An incomplete create row is status, not an interaction target; only a ready
// create action participates in selection and Enter dispatch.
//
// GSX files own visible template hierarchy. Target-config transitions live in
// effects.go, and pure projections accept concrete values rather than reading
// reactive component state. Azure alone retains a narrow *_component.go adapter
// because its mounted receiver owns endpoint-draft continuity that go-tui cannot
// yet express directly; the adapter is deleted when the generator supports it.
package target_config
