package telemetry

import (
	"fmt"
	"io"
	"strings"
)

const firstRunNoticeText = `Swobu runs as a local boundary between clients and model backends.
It sends anonymous aggregate reliability and usage summaries by default (opt-out).

Never sent:
- prompts
- responses
- request or response bodies
- API keys
- Authorization headers
- raw headers
- raw config
- file paths
- repo names
- usernames
- hostnames

Status:
  swobu telemetry status

Disable:
  swobu telemetry off

Global opt-out:
  export DO_NOT_TRACK=true
`

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
