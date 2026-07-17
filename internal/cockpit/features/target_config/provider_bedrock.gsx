package target_config

templ (f *bedrockProviderForm) Render() {
	<div class="flex-col w-full" deps={f.target.Draft, f.target.BaseURL}>
		if f.target.ShouldRenderBedrockConnectionRow() { @BedrockConnectionControl(f.target) }
		if f.target.ShouldRenderBedrockRegionRow() { @BedrockRegionControl(f.target) }
		if f.target.ShouldRenderBedrockEndpointRow() { @BedrockEndpointControl(f.target) }
		if f.target.ShouldRenderBedrockAuthModeRow() { @BedrockAuthControl(f.target) }
		if f.target.ShouldRenderBedrockProfileRow() { @BedrockProfileControl(f.target) }
		if f.target.ShouldRenderBedrockCredentialRow() { <div key={credentialRegionKey(f.target)} class="w-full">@CredentialControlRegion(f.target)</div> }
	</div>
}
