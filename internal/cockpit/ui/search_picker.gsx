package ui

templ (p *SearchPicker) Render() {
	list := p.choiceList()
	win := list.Window()
	countLabel, hint := list.CountLabel(win), "↑↓ search"
	<div class="flex-col w-full" deps={p.Query}>
		if p.Title != "" {
			@SearchPickerTitleRow(p.Title)
		}
		@SearchPickerQueryRow(p.Query.Get())
		<div class="flex-col w-full">
			for i, row := range win.Rows {
				<div key={p.ID + ":option:" + choiceRowKey(row)} class="w-full">
					@SearchPickerOptionComponent(p, list, row, i == 0)
				</div>
			}
		</div>
		@SearchPickerFooterRow(countLabel, hint)
	</div>
}

templ SearchPickerTitleRow(title string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span>{title}</span>
	</div>
}

templ SearchPickerQueryRow(query string) {
	<div class="flex-row w-full mb-1">
		<span class="w-2"></span>
		<span class="w-18">search</span>
		<span class="grow truncate nowrap">{searchPickerQueryValue(query)}</span>
	</div>
}

func SearchPickerOptionComponent(p *SearchPicker, list *ChoiceList, row ChoiceRowModel, first bool) *ChoiceRow {
	return NewChoiceRow(p.ID+":option:"+choiceRowKey(row), list, row, list.AutoFocus && first)
}

templ SearchPickerFooterRow(countLabel string, hint string) {
	<div class="w-full mt-1">
		<div class="flex-row w-full">
			<span class="w-2"></span>
			<span class="grow truncate nowrap">{countLabel}</span>
			<span class="w-14">{hint}</span>
		</div>
	</div>
}
