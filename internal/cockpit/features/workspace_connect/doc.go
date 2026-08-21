// Package workspace_connect owns the inline workspace endpoint disclosure,
// client browse presentation, named-client plan preview and execution lifecycle,
// and manual setup inspection and copying for generic clients.
// Concrete discovery and foreign configuration mutation stay behind this
// consumer-owned narrow operations interface, satisfied by clientconnect.Service.
//
// Governing Interaction Laws:
//  1. Inline Disclosure Grammar: Exactly one child detail expands inline directly
//     beneath its browse row while sibling rows remain visible in the browse list.
//     Opening another client closes the previous detail.
//  2. Nearest-Scope Escape Navigation: Esc from an expanded child returns to the
//     browse list; Esc from the browse list closes the endpoint disclosure.
//  3. Foreign Configuration Truth: Configured state is re-inspected from the
//     underlying discovery service after Apply; no toasts, timers, or persistent
//     bindings are emitted.
//  4. Local Feedback Ownership: Copy results (copied, saved fallback path, or
//     error) are owned locally by the active leaf without emitting shell notices.
//  5. Compact Canvas Restraint: Resting collapsed endpoint row exposes "clients ↵"
//     and the "OpenAI · Anthropic" compatibility line; expanding the endpoint
//     switches the action to "close ↵" and suppresses the redundant ecosystem line.
//  6. Manual Setup Contract: Exposes the canonical workspace URL (Base URL),
//     default route model, canonical Models discovery URL, placeholder API key,
//     and API family.
package workspace_connect
