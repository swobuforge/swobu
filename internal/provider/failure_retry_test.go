package provider

import (
	"net/http"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestRetryNotBeforeParsesStandardRetryAfterForms(t *testing.T) {
	now := time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)
	date := now.Add(3 * time.Minute)
	cases := []struct {
		name string
		raw  string
		want time.Time
		ok   bool
	}{
		{name: "delta seconds", raw: "120", want: now.Add(2 * time.Minute), ok: true},
		{name: "HTTP date", raw: date.Format(http.TimeFormat), want: date, ok: true},
		{name: "invalid", raw: "tomorrow", ok: false},
		{name: "negative", raw: "-1", ok: false},
		{name: "expired date", raw: now.Add(-time.Minute).Format(http.TimeFormat), ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Unavailable(canonical.NewBackendError("a", 429, "unavailable", tc.raw))
			got, ok := RetryNotBefore(err, now)
			if ok != tc.ok || ok && !got.Equal(tc.want) {
				t.Fatalf("RetryNotBefore = (%s, %t), want (%s, %t)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestRetryNotBeforeIgnoresNonBackendFailure(t *testing.T) {
	if got, ok := RetryNotBefore(Unavailable(contextDeadlineError{}), time.Now()); ok || !got.IsZero() {
		t.Fatalf("RetryNotBefore = (%s, %t), want no fact", got, ok)
	}
}

type contextDeadlineError struct{}

func (contextDeadlineError) Error() string { return "transport timeout" }
