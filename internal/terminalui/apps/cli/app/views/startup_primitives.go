package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/view"
)

func SplashBlock(rows []string) view.ViewSpec {
	children := make([]view.ViewSpec, 0, len(rows))
	for _, row := range rows {
		children = append(children, view.DurableText(row))
	}
	return view.FlowColumn("splash", 0, children...)
}

func MessageBlock(title string, rows []string, wrapWidth int) view.ViewSpec {
	if wrapWidth < 20 {
		wrapWidth = 72
	}
	return view.DurablePanel(view.PanelSpec{
		Title:       strings.TrimSpace(title), // swobu:io-string source=boundary
		Rows:        append([]string(nil), rows...),
		TargetWidth: wrapWidth + 4,
		MinWidth:    20,
		PadLeft:     1,
		PadRight:    1,
		Border: view.PanelBorderStyle{
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
