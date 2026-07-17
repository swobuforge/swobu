package ui

templ (s *Select) Render() {
	<div class="flex-col w-full">
		@SelectHeaderComponent(s)

		if s.IsEntered() && s.props.Body != nil {
			<div class="pl-3 flex-col w-full">
				@SelectBodyComponent(s)
			</div>
		}
	</div>
}
