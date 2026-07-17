package target_config

templ (f *openAICompatibleProviderForm) Render() {
	<div class="flex-col w-full" deps={f.target.Draft, f.target.BaseURL}>
		if f.target.ShouldRenderEndpointRow() { @EndpointInput(f.target, f.target.setupState().RequiresEndpoint) }
		if f.target.ShouldRenderCredentialHeaderRow() { @CredentialHeaderControl(f.target) }
		if f.target.ShouldRenderOpenAICompatibleCredentialRow() { <div key={credentialRegionKey(f.target)} class="w-full">@CredentialControlRegion(f.target)</div> }
	</div>
}
