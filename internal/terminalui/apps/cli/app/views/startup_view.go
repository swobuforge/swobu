package views

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cli/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/model"
)

func Build(startup state.StartupState) model.Node {
	children := make([]model.Node, 0, len(startup.Sections)+2)
	for i, section := range startup.Sections {
		var node model.Node
		switch section.Kind {
		case "splash":
			node = SplashBlock(section.Rows)
		case "status":
			message := ""
			if len(section.Rows) > 0 {
				message = section.Rows[0]
			}
			node = StatusLine(section.Phase, message)
		default:
			node = MessageBlock(section.Title, section.Rows, 72)
		}
		node.Key = fmt.Sprintf("startup-section-%d", i)
		children = append(children, node)
	}
	if startup.Status != "" {
		children = append(children, Status(startup.Status))
	}
	return model.Node{Kind: "startup", Children: children}
}
