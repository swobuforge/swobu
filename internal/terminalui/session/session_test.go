package session

import (
	"bytes"
	"sync"
	"testing"
)

func TestSession_BindWriter_ModeMismatchFailsFast(t *testing.T) {
	t.Parallel()

	s := New(ModeTranscript)
	var out bytes.Buffer
	transcript := s.BindWriter(ModeTranscript, &out)
	interactive := s.BindWriter(ModeInteractive, &out)

	if _, err := transcript.Write([]byte("ok")); err != nil {
		t.Fatalf("transcript write failed: %v", err)
	}
	if _, err := interactive.Write([]byte("bad")); err == nil {
		t.Fatal("expected interactive write to fail in transcript mode")
	}
}

func TestSession_BindWriter_SerializesConcurrentWrites(t *testing.T) {
	t.Parallel()

	s := New(ModeTranscript)
	var out bytes.Buffer
	w := s.BindWriter(ModeTranscript, &out)

	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := w.Write([]byte("x")); err != nil {
				t.Errorf("write failed: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := len(out.String()); got != workers {
		t.Fatalf("output length = %d, want %d", got, workers)
	}
}

func TestSession_OnModeChange_Notified(t *testing.T) {
	t.Parallel()

	s := New(ModeTranscript)
	var got [][2]Mode
	s.OnModeChange(func(prev, next Mode) {
		got = append(got, [2]Mode{prev, next})
	})
	if err := s.SetMode(ModeInteractive); err != nil {
		t.Fatalf("set mode interactive: %v", err)
	}
	if err := s.SetMode(ModeTranscript); err != nil {
		t.Fatalf("set mode transcript: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("mode changes = %d, want 2", len(got))
	}
	if got[0] != [2]Mode{ModeTranscript, ModeInteractive} {
		t.Fatalf("first transition = %#v", got[0])
	}
	if got[1] != [2]Mode{ModeInteractive, ModeTranscript} {
		t.Fatalf("second transition = %#v", got[1])
	}
}
