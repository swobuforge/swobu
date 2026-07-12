package compat

// Decision records what happened to one feature during one exchange.
type Decision struct {
	Feature Feature `json:"feature"`
	Outcome Outcome `json:"outcome"`
	Subject Subject `json:"subject,omitempty"`
}
