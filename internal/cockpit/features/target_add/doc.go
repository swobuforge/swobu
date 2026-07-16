// Package target_add owns the provider-setup-plus-model-selection workflow for
// creating a new route target.
//
// The workflow is mounted by the routes section when the operator activates
// "add target" on an expanded route. It is not a global modal; it renders
// inline under the route row. Provider setup, auth, model catalog probing, and
// placement choices are all internal to this package. The route section only
// knows whether a workflow is open for a given route. Empty routes skip the
// placement chooser and create their first target at step 1.
package target_add
