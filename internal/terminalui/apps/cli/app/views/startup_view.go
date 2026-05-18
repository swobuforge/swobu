package views

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cli/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/view"
)

func Build(startup state.StartupState) view.ViewSpec {
	children := make([]view.ViewSpec, 0, len(startup.Sections)+2)
	for i, section := range startup.Sections {
		var node view.ViewSpec
		if section.Kind == "splash" {
			node = SplashBlock(section.Rows)
		} else {
			node = MessageBlock(section.Title, section.Rows, 72)
		}
		node.Key = fmt.Sprintf("startup-section-%d", i)
		children = append(children, node)
	}
	return view.FlowColumn("startup", 0, children...)
}
