package target_config

import (
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
)

templ (w *TargetConfig) Render() {
	<div class="flex-col w-full" deps={w.Phase}>
		if w.IsOpen() {
			<div key={TargetAddMountKey(w, "target-config-parent")} class="w-full">
				@TargetConfigHeader(w)
			</div>
			<div class="pl-3 flex-col w-full" deps={w.Draft}>
				if strings.TrimSpace(w.Draft.Get().ProviderSpec) == "" {
					@ProviderSelect(w)
				} else {
					@ProviderSummary(w)
					if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecAzure {
						@AzureProviderForm(w)
					} else if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecOpenAICompatible {
						@OpenAICompatibleProviderForm(w)
					} else if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecBedrock {
						@BedrockProviderForm(w)
					} else if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecChatGPT {
						@ChatGPTProviderForm(w)
					} else {
						@HTTPProviderForm(w)
					}
					if w.ShouldRenderTargetTail() { @TargetConfigTail(w) }
					@TargetConfigError(w)
				}
			</div>
		}
	</div>
}
