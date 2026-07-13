// swobu:lint ignore test-only-dead-cluster because=framework core API types are the retained engine root, not dead architecture residue.
package core

import "context"

// EffectKey is the stable identity token for one declared effect.
type EffectKey string

// EffectPolicy selects how the runtime should handle duplicate or concurrent
// effects sharing the same key.
type EffectPolicy uint8

const (
	// EffectRunOnce allows the effect to run whenever emitted. No deduplication.
	EffectRunOnce EffectPolicy = iota
	// EffectCancelPrevious cancels any previously running effect with the same
	// key before starting the new one.
	EffectCancelPrevious
	// EffectIgnoreWhileRunning drops new emissions while an effect with the
	// same key is already running.
	EffectIgnoreWhileRunning
)

// Effect[E] declares one unit of external work the runtime must perform.
// Effects are structured: the runtime owns goroutine lifecycle, panic
// recovery, context cancellation, and follow-up event delivery.
type Effect[E any] struct {
	// Key is the stable identity used for deduplication and policy matching.
	// An empty key is invalid; constructors must supply one.
	Key EffectKey

	// Policy controls concurrent and duplicate behavior.
	Policy EffectPolicy

	// Run executes the effect and returns one event of type E.
	// The runtime supplies a context for cancellation.
	Run func(ctx context.Context) E
}

// Empty reports whether the effect has no meaningful key.
func (e Effect[E]) Empty() bool { return e.Key == "" }

// App[S, E] is the pure typed application contract.
//
// The framework owns the terminal, input decoding, layout, focus, and
// effect runtime. The app owns state transitions, view construction, and
// semantic meaning.
type App[S any, E any] interface {
	// Init returns the initial state and any startup effects.
	Init() (S, []Effect[E])

	// Update handles one event and returns the new state plus any effects.
	Update(S, E) (S, []Effect[E])

	// View builds the semantic UI tree from the current state.
	// The returned node is an intent tree; the framework compiles it to
	// terminal frames.
	View(S) Node[E]
}

// MutableApp[S, E] is an optional performance-oriented variant for Go
// environments where state copying is expensive.
//
// This is a concession to Go, not the semantic center. New apps should
// prefer App[S, E] whenever possible.
type MutableApp[S any, E any] interface {
	// Init populates initial state into tx and may queue effects.
	Init(tx *Tx[S, E])

	// Update reads the current state from tx, handles the event, and may
	// mutate tx state or queue effects.
	Update(tx *Tx[S, E], event E)

	// View builds the semantic UI tree from the current state.
	View(*S) Node[E]
}

// Tx[S, E] is the transaction handle used by MutableApp.
// It is owned by the runtime and passed into the app during Init/Update.
type Tx[S any, E any] struct {
	// State is the mutable app state. The runtime initializes it before
	// calling Init and preserves it across Update calls.
	State *S

	// effects accumulates declared effects during one transaction.
	effects []Effect[E]
}

// Effect appends one effect to the transaction.
func (tx *Tx[S, E]) Effect(e Effect[E]) {
	tx.effects = append(tx.effects, e)
}

// Effects returns the accumulated effects and clears the transaction buffer.
func (tx *Tx[S, E]) Effects() []Effect[E] {
	out := tx.effects
	tx.effects = tx.effects[:0]
	return out
}

// RuntimeEvent[E] distinguishes application events from framework runtime
// events at the event boundary.
type RuntimeEvent[E any] struct {
	// App holds an application-typed event when Kind is AppEvent.
	App E

	// Runtime holds a framework-typed event when Kind is RuntimeEvent.
	Runtime RuntimeKind
}

// RuntimeKind enumerates framework-owned event sources.
type RuntimeKind uint8

const (
	// RuntimeResize emits when the terminal dimensions change.
	RuntimeResize RuntimeKind = iota
	// RuntimeTick emits on a scheduled timer tick.
	RuntimeTick
	// RuntimeFocusChange emits when the focus target changes.
	RuntimeFocusChange
	// RuntimeEffectResult wraps an effect completion.
	RuntimeEffectResult
)
