package telemetry

import (
	"fmt"
	"io"
	"strings"
)

// firstRunNoticeText is the single non-blocking one-line telemetry notice,
// printed once on first daemon start. Telemetry is anonymous by construction
// (aggregate counters only — no content, no identifiers), so this is an FYI,
// not a gate: the daemon starts regardless of notice state.
const firstRunNoticeText = "Anonymous aggregate telemetry on (counts only — no content, no identifiers). Turn off: swobu telemetry off or DO_NOT_TRACK."

func FirstRunNoticeText() string {
	return firstRunNoticeText
}

func (s Store) EnsureNoticeShownWithDisclosure(out io.Writer) (State, error) {
	state, err := s.LoadOrCreate()
	if err != nil {
		return State{}, err
	}
	if state.NoticeShown {
		return state, nil
	}
	if out == nil {
		out = io.Discard
	}
	if _, err := fmt.Fprintln(out, strings.TrimSpace(firstRunNoticeText)); err != nil { // swobu:io-string source=boundary
		return State{}, err
	}
	return s.MarkNoticeShown()
}
