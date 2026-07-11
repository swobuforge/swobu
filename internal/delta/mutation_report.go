package delta

// MutationReport groups mutation rows for one observed exchange path segment.
type MutationReport struct {
	Mutations []MutationRecord `json:"mutations"`
}
