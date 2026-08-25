package ui

import "strings"

templ (s *Select) Render() {
	<div class="flex-col w-full">
		@SelectHeaderComponent(s)
		if !s.IsEntered() && strings.TrimSpace(s.props.Detail) != "" {
			<div class="pl-20 w-full">
				@FlowText(s.props.Detail)
			</div>
		}

		if s.IsEntered() && s.props.Body != nil {
			<div class="pl-3 flex-col w-full">
				@SelectBodyComponent(s)
			</div>
		}
	</div>
}
