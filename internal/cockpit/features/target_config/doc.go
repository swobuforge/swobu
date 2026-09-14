// Package target_config implements Cockpit target configuration.
//
// The routes section mounts this inline editor. Provider profile facts govern
// connection authoring; model identity does not choose endpoint semantics.
// Authentication sessions belong to the form lifecycle and are cancelled when
// replaced. Credential inputs author opaque references, not stored credential
// values; filesystem interpretation belongs to the daemon.
package target_config
