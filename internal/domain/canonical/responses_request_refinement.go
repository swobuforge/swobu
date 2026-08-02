package canonical

// ResponsesRequestRefinement carries official Responses request decisions that
// are read only by Responses lowering or continuation policy.
type ResponsesRequestRefinement struct {
	store Specified[bool]
}

// NewResponsesRequestRefinement constructs request-scoped Responses truth.
func NewResponsesRequestRefinement(store Specified[bool]) ResponsesRequestRefinement {
	return ResponsesRequestRefinement{store: cloneSpecified(store, func(value bool) bool { return value })}
}

// Store returns the explicit official store value and whether it was supplied.
func (r ResponsesRequestRefinement) Store() (bool, bool) { return r.store.Get() }

// StoreField preserves omission independently from an explicit false value.
func (r ResponsesRequestRefinement) StoreField() Specified[bool] {
	return cloneSpecified(r.store, func(value bool) bool { return value })
}

// PersistenceEligible reports whether explicit request policy permits
// provider-side continuation storage. Omission does not claim prohibition.
func (r ResponsesRequestRefinement) PersistenceEligible() bool {
	store, specified := r.Store()
	return !specified || store
}

// Clone returns an independent value copy.
func (r ResponsesRequestRefinement) Clone() ResponsesRequestRefinement {
	return NewResponsesRequestRefinement(r.StoreField())
}
