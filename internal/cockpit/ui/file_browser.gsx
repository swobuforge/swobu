package ui

templ (b *FileBrowser) Render() {
	win := b.Window()
	list := b.choiceList()
	<div class="flex-col w-full" deps={b.Query, b.CurrentDir, b.Error}>
		if b.Title != "" {
			@FileBrowserTitleRow(b.Title)
		}
		@FileBrowserDirRow(win.CurrentDir)
		@FileBrowserSearchRow(win.Query)
		<div class="flex-col w-full">
			for i, row := range list.Window().Rows {
				<div key={b.ID + ":entry:" + choiceRowKey(row)} class="w-full">
					@FileBrowserEntryComponent(b, list, row, i == 0)
				</div>
			}
		</div>
		if win.HasError {
			@FileBrowserErrorRow(win.ErrorText)
		}
		@FileBrowserHintRow(fileBrowserCountLabel(win.ShownRows, win.TotalRows))
	</div>
}

templ FileBrowserTitleRow(title string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span class="grow">{title}</span>
	</div>
}

templ FileBrowserDirRow(dir string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span class="w-18">dir</span>
		<span class="grow truncate nowrap">{dir}</span>
	</div>
}

templ FileBrowserSearchRow(query string) {
	<div class="flex-row w-full mb-1">
		<span class="w-2"></span>
		<span class="w-18">search</span>
		<span class="grow truncate nowrap">{searchPickerQueryValue(query)}</span>
	</div>
}

func FileBrowserEntryComponent(b *FileBrowser, list *ChoiceList, row ChoiceRowModel, first bool) *ChoiceRow {
	return NewChoiceRow(b.ID+":entry:"+choiceRowKey(row), list, row, list.AutoFocus && first)
}

templ FileBrowserErrorRow(msg string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span class="grow truncate nowrap">{msg}</span>
	</div>
}

templ FileBrowserHintRow(countLabel string) {
	<div class="flex-row w-full mt-1">
		<span class="w-2"></span>
		<span class="grow truncate nowrap">{countLabel}</span>
		<span class="w-14">↑↓ choose</span>
	</div>
}
