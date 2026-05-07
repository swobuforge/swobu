package session

import (
	"fmt"
	"io"
	"sync"
)

type Mode string

const (
	ModeTranscript  Mode = "transcript"
	ModeInteractive Mode = "interactive"
)

type Session struct {
	mu        sync.Mutex
	mode      Mode
	listeners []func(prev Mode, next Mode)
}

func New(initial Mode) *Session {
	if initial == "" {
		initial = ModeTranscript
	}
	return &Session{mode: initial}
}

func (s *Session) Mode() Mode {
	if s == nil {
		return ModeTranscript
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

func (s *Session) SetMode(next Mode) error {
	if s == nil {
		return nil
	}
	if next == "" {
		return fmt.Errorf("session mode is required")
	}
	s.mu.Lock()
	prev := s.mode
	if prev == next {
		s.mu.Unlock()
		return nil
	}
	s.mode = next
	listeners := append([]func(prev Mode, next Mode){}, s.listeners...)
	s.mu.Unlock()
	for _, listener := range listeners {
		if listener != nil {
			listener(prev, next)
		}
	}
	return nil
}

func (s *Session) OnModeChange(listener func(prev Mode, next Mode)) {
	if s == nil || listener == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

func (s *Session) BindWriter(mode Mode, target io.Writer) io.Writer {
	if s == nil || target == nil {
		return target
	}
	// A bound writer is a phase guard: writes fail fast when attempted from the
	// wrong session mode.
	return boundWriter{session: s, mode: mode, target: target}
}

type boundWriter struct {
	session *Session
	mode    Mode
	target  io.Writer
}

func (w boundWriter) Write(p []byte) (int, error) {
	if w.session == nil || w.target == nil {
		return 0, nil
	}
	// Reuse the session lock so mode checks and writes are atomic relative to
	// mode transitions and sibling writes.
	w.session.mu.Lock()
	defer w.session.mu.Unlock()
	if w.session.mode != w.mode {
		return 0, fmt.Errorf("terminal session mode mismatch: current=%s write=%s", w.session.mode, w.mode)
	}
	return w.target.Write(p)
}
