// Package inspector provides a dev-mode diagnostic overlay for core.Node trees.
//
// It renders three view modes:
//   - diagnostics: validation errors and warnings
//   - layout: bounding box annotations for every semantic node
//   - focus: semantic focus graph with scopes, groups, and focusable nodes
//
// The inspector is gated behind the SWOBU_INSPECTOR environment variable.
// Production codepaths must not reference this package.
package inspector
