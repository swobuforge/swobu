// Package target_config implements Cockpit target configuration.
//
// The routes section mounts this inline editor. Provider profile facts govern
// connection authoring; model identity does not choose endpoint semantics.
// Authentication sessions belong to the form lifecycle and are cancelled when
// replaced. Credential inputs author opaque references, not stored credential
// values; filesystem interpretation belongs to the daemon. Routing placement
// is draft state anchored to the current route topology. Edit refreshes follow
// durable topology unless an uncommitted explicit placement remains valid;
// create refreshes retain an expressible draft placement.
package target_config
