package compat

// Support describes the support level for one feature on one route.
type Support string

const (
	Supported   Support = "supported"
	Unsupported Support = "unsupported"
	Partial     Support = "partial"
	Unknown     Support = "unknown"
)

// Capability describes one route-scoped support fact for one feature.
type Capability struct {
	Feature    Feature           `json:"feature"`
	Support    Support           `json:"support"`
	Provider   string            `json:"provider,omitempty"`
	Protocol   string            `json:"protocol,omitempty"`
	Model      string            `json:"model,omitempty"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
}
