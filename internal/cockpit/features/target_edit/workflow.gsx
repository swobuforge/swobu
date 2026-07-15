package target_edit

import tui "github.com/grindlemire/go-tui"

templ (w *Workflow) Render() {
	<div class="flex-col w-full" deps={w.Phase, w.Mode, w.Name, w.Provider, w.Model, w.BaseURL, w.Credential, w.Rank, w.Weight, w.Error}>
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">name</span>
			if app != nil {
				<input value={w.Name} width={30} border={tui.BorderRounded} />
			} else {
				<span class="w-30">{w.Name.Get()}</span>
			}
		</div>
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">provider</span>
			if app != nil {
				<input value={w.Provider} width={30} border={tui.BorderRounded} />
			} else {
				<span class="w-30">{w.Provider.Get()}</span>
			}
		</div>
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">{w.ModelLabel()}</span>
			if app != nil {
				<input value={w.Model} width={30} border={tui.BorderRounded} />
			} else {
				<span class="w-30">{w.Model.Get()}</span>
			}
		</div>
		if w.ShowBaseURL() {
			<div class="flex-row w-full">
				<span class="w-8"></span>
				<span class="w-15">{w.BaseURLLabel()}</span>
				if app != nil {
					<input value={w.BaseURL} width={30} border={tui.BorderRounded} />
				} else {
					<span class="w-30">{w.BaseURL.Get()}</span>
				}
			</div>
		}
		if w.ShowAuthDisclosure() {
			<div class="flex-row w-full">
				<span class="w-8"></span>
				<span class="w-15">auth</span>
				<span class="w-30">{w.AuthDisclosureValue()}</span>
			</div>
		}
		if w.ShowDeviceCode() {
			<div class="flex-row w-full">
				<span class="w-8"></span>
				<span class="w-15">code</span>
				<span class="w-30">{w.DeviceCodeValue()}</span>
			</div>
		}
		if w.ShowCredential() {
			<div class="flex-row w-full">
			<span class="w-8"></span>
				<span class="w-15">{w.CredentialLabel()}</span>
				if app != nil && w.AuthShape() != AuthShapeBedrock {
					<input value={w.Credential} width={30} border={tui.BorderRounded} />
				} else {
					<span class="w-30">{w.CredentialValue()}</span>
				}
			</div>
		}
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">rank</span>
			if app != nil {
				<input value={w.Rank} width={30} border={tui.BorderRounded} />
			} else {
				<span class="w-30">{w.Rank.Get()}</span>
			}
		</div>
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">weight</span>
			if app != nil {
				<input value={w.Weight} width={30} border={tui.BorderRounded} />
			} else {
				<span class="w-30">{w.Weight.Get()}</span>
			}
		</div>
		<div class="flex-row w-full focusable" onActivate={w.ActivateSave}>
			<span class="w-8"></span>
			<span class="w-15">target</span>
			<span class="w-36">{w.targetName()}</span>
			<span>{w.SaveActionLabel()}</span>
		</div>
		<div class="flex-row w-full focusable" onActivate={w.ActivateDelete}>
			<span class="w-8"></span>
			<span class="w-15">delete</span>
			<span class="w-36">{w.DeleteValueLabel()}</span>
			<span>{w.DeleteActionLabel()}</span>
		</div>
		if w.Error.Get() != "" {
			<div class="flex-row w-full">
				<span class="w-8"></span>
				<span>{w.Error.Get()}</span>
			</div>
		}
	</div>
}
