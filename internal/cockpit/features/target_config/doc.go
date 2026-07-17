// Package target_config owns the mounted component for creating and editing
// route targets.
//
// TargetConfig is mounted by the routes section when the operator activates
// "add target" on an expanded route. It is not a global modal; it renders
// inline under the route row. Provider setup, auth, model catalog probing, and
// routing choices backed by placement ranks are all internal to this package.
// Once a provider is selected, provider-specific GSX components render the
// visible row sequence below provider selection. The route section only knows
// whether a target config is open for a given route. Empty
// routes skip the routing chooser and create their first target at step 1.
// The Bedrock component renders only the fixed Mantle connection; native Bedrock
// Runtime is not a Cockpit target-creation path.
//
// GSX files own visible template hierarchy only. Target-config transitions and
// state-derived helpers live in semantically named Go sources; pure projections
// accept concrete values rather than reading reactive component state.
package target_config
