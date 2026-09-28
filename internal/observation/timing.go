package observation

import (
	"fmt"
	"time"
)

type Timing struct {
	startedAt, firstByteAt, endedAt    time.Time
	hasStarted, hasFirstByte, hasEnded bool
}

func NewUnknownTiming() Timing              { return Timing{} }
func NewTimingStartedAt(a time.Time) Timing { t := Timing{}; t.MarkStarted(a); return t }
func NewTimingWithOptional(ttfb, dur *int) (Timing, error) {
	t := Timing{}
	b := time.Unix(0, 0)
	if ttfb != nil {
		if *ttfb < 0 {
			return Timing{}, fmt.Errorf("ttfb must not be negative")
		}
		t.startedAt = b
		t.firstByteAt = b.Add(time.Duration(*ttfb) * time.Millisecond)
		t.hasStarted = true
		t.hasFirstByte = true
	}
	if dur != nil {
		if *dur < 0 {
			return Timing{}, fmt.Errorf("duration must not be negative")
		}
		if !t.hasStarted {
			t.startedAt = b
			t.hasStarted = true
		}
		t.endedAt = b.Add(time.Duration(*dur) * time.Millisecond)
		t.hasEnded = true
	}
	if t.hasFirstByte && t.hasEnded && t.endedAt.Before(t.firstByteAt) {
		return Timing{}, fmt.Errorf("duration must not be less than ttfb")
	}
	return t, nil
}
func (t *Timing) MarkStarted(a time.Time) {
	if t == nil || a.IsZero() || t.hasStarted {
		return
	}
	t.startedAt = a
	t.hasStarted = true
}
func (t *Timing) MarkFirstByte(a time.Time) {
	if t == nil || a.IsZero() || !t.hasStarted || t.hasFirstByte {
		return
	}
	t.firstByteAt = a
	t.hasFirstByte = true
}
func (t *Timing) MarkEnded(a time.Time) {
	if t == nil || a.IsZero() {
		return
	}
	t.endedAt = a
	t.hasEnded = true
}
func (t Timing) StartedAt() (time.Time, bool)   { return t.startedAt, t.hasStarted }
func (t Timing) FirstByteAt() (time.Time, bool) { return t.firstByteAt, t.hasFirstByte }
func (t Timing) EndedAt() (time.Time, bool)     { return t.endedAt, t.hasEnded }
func (t Timing) TTFBMillis() (int, bool) {
	if !t.hasStarted || !t.hasFirstByte || t.firstByteAt.Before(t.startedAt) {
		return 0, false
	}
	return int(t.firstByteAt.Sub(t.startedAt) / time.Millisecond), true
}
func (t Timing) DurationMillis() (int, bool) {
	if !t.hasStarted || !t.hasEnded || t.endedAt.Before(t.startedAt) {
		return 0, false
	}
	return int(t.endedAt.Sub(t.startedAt) / time.Millisecond), true
}
