package producttelemetry

import (
	"testing"
	"time"
)

func TestAgeBucket(t *testing.T) {
	cases := map[time.Duration]string{
		1 * time.Hour:        "<1d",
		2 * 24 * time.Hour:   "1_7d",
		10 * 24 * time.Hour:  "8_30d",
		45 * 24 * time.Hour:  "31_90d",
		120 * 24 * time.Hour: "90d_plus",
	}
	for age, want := range cases {
		if got := AgeBucket(age); got != want {
			t.Errorf("AgeBucket(%v) = %q, want %q", age, got, want)
		}
	}
}
