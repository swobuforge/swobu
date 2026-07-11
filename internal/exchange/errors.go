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
