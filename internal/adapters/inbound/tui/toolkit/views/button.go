package views

import (
	"strings"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
)

// ButtonRenderNode is a thin preset over the generic Action primitive.
type ButtonRenderNode struct{ *ActionRenderNode }

func NewButton(label string, onActivate func() []update.Action) *ButtonRenderNode {
	trimmed := strings.TrimSpace(label)
	intrinsic := runeLen(trimmed) + 4
	return &ButtonRenderNode{
		ActionRenderNode: NewAction(intrinsic, true, true, func(focused bool, _ int) string {
			left, right := "[", "]"
			if focused {
				left, right = ">", "<"
			}
			return left + " " + trimmed + " " + right
		}, func(string) []update.Action {
			if onActivate == nil {
				return nil
			}
			return onActivate()
		}, nil),
	}
}
