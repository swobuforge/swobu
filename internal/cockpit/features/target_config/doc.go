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
// reconciliation. Z.AI retains its open-set authored model and never initiates
// catalog discovery. Bedrock keeps region and optional explicit API URL as
// separate draft facts. An empty endpoint stays empty through edit and save;
// the row displays the resolver-derived effective URL without materializing it
// into generic BaseURL state. Endpoint input is editor-local until successful
// submission, and submitting the regional default canonicalizes back to empty.
// Bedrock has one authentication field: an absent credential reference
// selects AWS identity, while a present reference selects a bearer API key and
// embeds the shared credential chooser. ChatGPT retains its genuine
// login-session workflow. Browser login is the default and device code is an
// explicit form-owned choice. Pending authentication remains selectable;
// changing mode cancels the active session before starting its replacement. A
// pending session always renders its complete login URL as wrapped terminal
// text nested beneath one best-effort open action; auth choices use the normal
// child-row indentation without a placeholder label column. Opener failure
// does not mutate the form, so the visible URL remains the manual-selection
// hedge. A
// mounted pending ChatGPT session is observed through the local daemon until it
// succeeds or fails; manual refresh remains a recovery action, and the observer
// ends with the form, mount, app, or session.
// Custom Endpoint uses best-effort discovery plus the open-set model picker;
// Z.AI uses the same picker without discovery. In both cases the
// operator-authored model ID remains authoritative.
// An incomplete create row is status, not an interaction target; only a ready
// create action participates in selection and Enter dispatch.
//
// GSX files own visible template hierarchy. Target-config transitions live in
// effects.go, and pure projections accept concrete values rather than reading
// reactive component state. Azure alone retains a narrow *_component.go adapter
// because its mounted receiver owns endpoint-draft continuity that go-tui cannot
// yet express directly; the adapter is deleted when the generator supports it.
package target_config
