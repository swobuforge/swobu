package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/transcript"
)

func SplashBlock(rows []string) transcript.ViewSpec {
	children := make([]transcript.ViewSpec, 0, len(rows))
	for _, row := range rows {
		children = append(children, transcript.DurableText(row))
	}
	return transcript.FlowColumn("splash", 0, children...)
}

func MessageBlock(title string, rows []string, wrapWidth int) transcript.ViewSpec {
	if wrapWidth < 20 {
		wrapWidth = 72
	}
	return transcript.DurablePanel(transcript.PanelSpec{
		Title:       strings.TrimSpace(title), // swobu:io-string source=boundary
		Rows:        append([]string(nil), rows...),
		TargetWidth: wrapWidth + 4,
		MinWidth:    20,
		PadLeft:     1,
		PadRight:    1,
		Border: transcript.PanelBorderStyle{
			TopLeft:      "╭",
			TopRight:     "╮",
			BottomLeft:   "╰",
			BottomRight:  "╯",
			Horizontal:   "─",
			Vertical:     "│",
			TitlePrefix:  "─ ",
			TitleSuffix:  " ",
			FallbackName: "Box",
		},
	})
}
