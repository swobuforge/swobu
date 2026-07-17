package ui

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
)

func TestSelectNotEnteredHidesBodyAndEnteredShowsIt(t *testing.T) {
	s := NewSelect(SelectProps{
		ID:    "proto",
		Label: "protocol",
		Value: "",
		Body: func(backout func()) tui.Component {
			return NewSearchPicker("proto-picker", "protocol", []SearchOption{
				{ID: "responses_stream", Label: "responses_stream"},
				{ID: "responses", Label: "responses"},
			}, func(Selection) {}, backout)
		},
	})

	notEntered, err := mountedrender.String(s, 64, 12)
	if err != nil {
		t.Fatalf("not entered render: %v", err)
	}
	if !strings.Contains(notEntered, "protocol") || !strings.Contains(notEntered, "choose") {
		t.Fatalf("not entered row should show label + choose action:\n%s", notEntered)
	}
	if strings.Contains(notEntered, "responses_stream") {
		t.Fatalf("not entered render must not include body options:\n%s", notEntered)
	}

	s.Enter()
	entered, err := mountedrender.String(s, 64, 12)
	if err != nil {
		t.Fatalf("entered render: %v", err)
	}
	if !strings.Contains(entered, "responses_stream") {
		t.Fatalf("entered render should include body options:\n%s", entered)
	}

	s.Backout()
	again, err := mountedrender.String(s, 64, 12)
	if err != nil {
		t.Fatalf("backout render: %v", err)
	}
	if strings.Contains(again, "responses_stream") {
		t.Fatalf("backout render must not include body options:\n%s", again)
	}
}
