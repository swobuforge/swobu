package compat

// Support describes the support level for one feature on one route.
type Support string

const (
	Supported   Support = "supported"
	Unsupported Support = "unsupported"
	Partial     Support = "partial"
	Unknown     Support = "unknown"
)
