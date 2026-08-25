package target_config

import (
	"strings"

	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type FlowTextView = ui.FlowTextView

func FlowText(text string) *FlowTextView {
	return ui.FlowText(text)
}

templ TargetConfigError(w *TargetConfig) {
	<div deps={w.Error}>
		if strings.TrimSpace(w.Error.Get()) != "" {
			<div class="pl-18 w-full">
				@FlowText(w.Error.Get())
			</div>
		}
	</div>
}
