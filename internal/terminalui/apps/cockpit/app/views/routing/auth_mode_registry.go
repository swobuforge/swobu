package routing

import (
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// authModeDescriptorSpec is the universal schema entry for one auth mode option.
type authModeDescriptorSpec struct {
	Mode        profile.AuthMode
	Label       string
	Interactive bool
}

func authModeDescriptorsForSpec(providerSpec string) []authModeDescriptorSpec {
	modes := profile.SupportedAuthModesForSpec(strings.TrimSpace(providerSpec)) // swobu:io-string source=boundary
	out := make([]authModeDescriptorSpec, 0, len(modes))
	for _, mode := range modes {
		out = append(out, authModeDescriptorSpec{
			Mode:        mode,
			Label:       authModeDisplayLabel(mode),
			Interactive: profile.IsInteractiveAuthMode(mode),
		})
	}
	return out
}

type authModeRowRenderer interface {
	RenderCreateExtras(providerSpec string, credentialRef string) []retained.ViewSpec[state.Model]
	RenderAddModelExtras(providerSpec string, credentialRef string) []retained.ViewSpec[state.Model]
}

type noopAuthModeRowRenderer struct{}

func (noopAuthModeRowRenderer) RenderCreateExtras(_ string, _ string) []retained.ViewSpec[state.Model] {
	return nil
}

func (noopAuthModeRowRenderer) RenderAddModelExtras(_ string, _ string) []retained.ViewSpec[state.Model] {
	return nil
}

type envAuthModeRowRenderer struct{}

func (envAuthModeRowRenderer) RenderCreateExtras(providerSpec string, credentialRef string) []retained.ViewSpec[state.Model] {
	return []retained.ViewSpec[state.Model]{
		retained.Named[state.Model]("env-key", providerEnvKeyRow(providerEnvKeyRowSpec{CreateMode: true})),
	}
}

func (envAuthModeRowRenderer) RenderAddModelExtras(providerSpec string, credentialRef string) []retained.ViewSpec[state.Model] {
	key := strings.TrimSpace(envCredentialKey(credentialRef)) // swobu:io-string source=boundary
	if key == "" {
		key = strings.TrimSpace(profile.DefaultEnvKeyForSpec(providerSpec)) // swobu:io-string source=boundary
	}
	if key == "" {
		return nil
	}
	return []retained.ViewSpec[state.Model]{
		retained.Named[state.Model]("add-model/env-expected", views.RowStatic("expected", key)),
	}
}

type fileAuthModeRowRenderer struct{}

func (fileAuthModeRowRenderer) RenderCreateExtras(providerSpec string, credentialRef string) []retained.ViewSpec[state.Model] {
	return []retained.ViewSpec[state.Model]{
		retained.Named[state.Model]("credential-file", providerCredentialFileBrowseRow(providerCredentialFileBrowseRowSpec{CreateMode: true})),
	}
}

func (fileAuthModeRowRenderer) RenderAddModelExtras(providerSpec string, credentialRef string) []retained.ViewSpec[state.Model] {
	path := strings.TrimSpace(credentialFilePath(credentialRef)) // swobu:io-string source=boundary
	if path == "" {
		return []retained.ViewSpec[state.Model]{retained.Named[state.Model]("add-model/file-missing", views.RowStatic("key file", "not found"))}
	}
	if _, err := os.Stat(path); err != nil {
		return []retained.ViewSpec[state.Model]{retained.Named[state.Model]("add-model/file-missing", views.RowStatic("key file", "not found"))}
	}
	return nil
}

type keychainAuthModeRowRenderer struct{}

func (keychainAuthModeRowRenderer) RenderCreateExtras(providerSpec string, credentialRef string) []retained.ViewSpec[state.Model] {
	return []retained.ViewSpec[state.Model]{
		retained.Named[state.Model]("keychain", providerKeychainKeyNameRow(providerKeychainKeyNameRowSpec{CreateMode: true})),
	}
}

func (keychainAuthModeRowRenderer) RenderAddModelExtras(providerSpec string, credentialRef string) []retained.ViewSpec[state.Model] {
	return nil
}

var authModeRowRenderers = map[string]authModeRowRenderer{
	"env":      envAuthModeRowRenderer{},
	"file":     fileAuthModeRowRenderer{},
	"keychain": keychainAuthModeRowRenderer{},
}

func authModeRendererForCredentialRef(credentialRef string) authModeRowRenderer {
	source := strings.TrimSpace(credentialSource(credentialRef)) // swobu:io-string source=boundary
	if renderer, ok := authModeRowRenderers[source]; ok {
		return renderer
	}
	return noopAuthModeRowRenderer{}
}
