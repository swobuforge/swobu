// Package app is the namespace root for the cockpit app subtree.
//
// The real ownership lives in child packages:
//
// - `app/state`: durable app state, actions, commands, reducer
// - `app/selectors`: pure derived view state
// - `app/views`: app-owned shell, section views, and root composition
//
// This root package intentionally does not provide a compatibility facade.
// Internal callers should import the owning child package directly.
package app
