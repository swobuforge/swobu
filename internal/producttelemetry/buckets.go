package producttelemetry

import "time"

// AgeBucket collapses an installation age into a coarse bucket.
func AgeBucket(age time.Duration) string {
	switch {
	case age < 24*time.Hour:
		return "<1d"
	case age < 7*24*time.Hour:
		return "1_7d"
	case age < 30*24*time.Hour:
		return "8_30d"
	case age < 90*24*time.Hour:
		return "31_90d"
	default:
		return "90d_plus"
	}
}
