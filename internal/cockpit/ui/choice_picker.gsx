package ui

templ (p *ChoicePicker) Render() {
	win := p.list.Window()
	<div class="flex-col w-full">
		<div class="flex-col w-full">
			for _, row := range win.Rows {
				<div key={p.ID + ":option:" + choiceRowKey(row)} class="w-full">
					@ChoicePickerOptionComponent(p, row)
				</div>
			}
		</div>
		if win.TotalRows > win.ShownRows {
			@ChoicePickerFooterRow(p.list.CountLabel(win))
		}
	</div>
}

templ ChoicePickerFooterRow(countLabel string) {
	<div class="flex-row w-full mt-1">
		<span class="w-2"></span>
		<span class="grow truncate nowrap">{countLabel}</span>
		<span class="w-14">↑↓ choose</span>
	</div>
}
