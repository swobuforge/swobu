package help

import "github.com/swobuforge/swobu/internal/cockpit/readmodel"

templ View(model readmodel.HelpReadModel) {
	<div class="flex-col w-full">
		<div class="flex-row">
			<span class="w-2"></span>
			<span>help</span>
		</div>
		<br />
		@HelpRow("version", model.VersionValue(), "")
		<br />
		@HelpRow("docs", model.DocsValue(), linkAction(model.DocsURL))
		@HelpRow("community", model.CommunityValue(), linkAction(model.CommunityURL))
		@HelpRow("issue", model.IssueValue(), linkAction(model.IssueURL))
		@HelpRow("diagnostics", model.DiagnosticsValue(), model.DiagnosticsAction())
		<br />
		<br />
	</div>
}

templ HelpRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

func linkAction(url string) string {
	if url == "" {
		return ""
	}
	return "open ↵"
}
