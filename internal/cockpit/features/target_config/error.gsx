package target_config

import "strings"

templ TargetConfigError(w *TargetConfig) {
	<div deps={w.Error}>
		if strings.TrimSpace(w.Error.Get()) != "" {
			<div class="flex-row w-full">
				<span class="w-18"></span>
				<span>{w.Error.Get()}</span>
			</div>
		}
	</div>
}
