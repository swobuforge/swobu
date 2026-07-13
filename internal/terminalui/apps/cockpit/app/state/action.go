package state

// Action is the cockpit event union. New app-facing code should work with typed
// events; this alias exists as the migration boundary from update.Action.
type Action = any
