package exchange

// UnsupportedProjectionError indicates semantic request content cannot be
// represented on the selected provider wire surface.
type UnsupportedProjectionError struct {
	Field  string
	Reason string
}

func (e UnsupportedProjectionError) Error() string {
	if e.Field == "" {
		return e.Reason
	}
	if e.Reason == "" {
		return e.Field + ": unsupported projection"
	}
	return e.Field + ": " + e.Reason
}

// InvalidCarrierError indicates one carrier invariant failed before exchange
// orchestration could safely continue.
type InvalidCarrierError struct {
	Reason string
}

func (e InvalidCarrierError) Error() string { return e.Reason }

// TransformInvariantError indicates transform chain output violated runtime
// invariants (for example mutate-without-declaration).
type TransformInvariantError struct {
	TransformID string
	Reason      string
}

func (e TransformInvariantError) Error() string {
	if e.TransformID == "" {
		return e.Reason
	}
	if e.Reason == "" {
		return e.TransformID + ": transform invariant violation"
	}
	return e.TransformID + ": " + e.Reason
}
