package workspace_edit

import tui "github.com/grindlemire/go-tui"

type Workflow struct {
	Open *tui.State[bool]
}

func NewWorkflow() *Workflow {
	return &Workflow{
		Open: tui.NewState(false),
	}
}

func (w *Workflow) OpenEditor() {
	w.Open.Set(true)
}
