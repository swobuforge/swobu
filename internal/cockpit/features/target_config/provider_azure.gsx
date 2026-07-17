package target_config

templ (f *azureProviderForm) Render() {
	<div class="flex-col w-full" deps={f.target.Draft, f.target.BaseURL}>
		if f.target.ShouldRenderEndpointRow() { @AzureProjectEndpointDraftInput(f.target, f.endpointDraft, f.target.setupState().RequiresEndpoint, func() { f.endpointCommitted = true }) }
		if f.target.setupState().RequiresEndpoint { @AzureCredentialBlocked(f.target)
		} else { <div key={credentialRegionKey(f.target)} class="w-full">@CredentialControlRegion(f.target, f.endpointCommitted)</div> }
	</div>
}
