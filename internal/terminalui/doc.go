// Package terminalui is retained noninteractive startup/session residue.
//
// It has been reduced to noninteractive startup and session residue only.
// The active interactive cockpit is internal/cockpit, built on go-tui.
//
// Remaining subpackages and their status:
//   - apps/cli    : CLI startup presenter (noninteractive). Keep until
//     startup surface migrates to go-tui or plain writer.
//   - session     : Session/mode type definitions. Keep until CLI startup
//     path no longer imports them.
//   - transcript  : Line-oriented output primitives for startup rendering.
//     Keep until startup presenter migrates.
//   - engine/reconcile, engine/output : Transcript reconciler/output used
//     only by the CLI presenter.
//   - view/layout : Layout constraints used only by transcript rendering.
//
// Deleted interactive subpackages:
//   - apps/cockpit, core, component, components/*, toolkit,
//     engine/retained/*, view/retained, testharness
//
// No new code should import these packages except to maintain or delete the
// startup surface until it also migrates. Interactive TUI work belongs in
// internal/cockpit and follows docs/04-design/go-tui-canons.md.
package terminalui
