package target_config

import tui "github.com/grindlemire/go-tui"
type httpProviderForm struct{ target *TargetConfig }
func HTTPProviderForm(w *TargetConfig) tui.Component { return &httpProviderForm{target: w} }
templ (f *httpProviderForm) Render() {
	<div class="flex-col w-full" deps={f.target.Draft, f.target.BaseURL}>
		if shouldRenderEndpointRow(f.target) {
			@EndpointInput(f.target, setupRequiresLocator(f.target))
		}

		if credential, ok := ambientOrReferenceCredential(f.target); ok {
			<div key={credentialRegionKey(f.target)} class="w-full">@AmbientOrReferenceAuthentication(ambientOrReferenceAuthenticationProps(f.target, credential))</div>
		} else if genericCredentialRowVisible(f.target) {
			<div key={credentialRegionKey(f.target)} class="w-full">@CredentialControlRegion(f.target, setupRequiresCredential(f.target))</div>
		}
	</div>
}
