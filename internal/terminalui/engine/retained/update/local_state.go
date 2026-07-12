package update

// LocalStateChangedAction marks one build-scoped state mutation.
//
// The retained and component runtimes treat it as an invalidation signal.
type LocalStateChangedAction struct{}
