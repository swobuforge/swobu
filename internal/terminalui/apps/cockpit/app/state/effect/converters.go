package effect

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func argsToProviderConfig(pc stateModel.ProviderConfigSnapshot) (endpointintent.ProviderConfig, error) {
	ref, err := endpointintent.ParseProviderConfigRef(pc.Ref)
	if err != nil {
		return endpointintent.ProviderConfig{}, fmt.Errorf("provider ref: %w", err)
	}
	spec, err := endpointintent.ParseProviderSpec(pc.ProviderSpec)
	if err != nil {
		return endpointintent.ProviderConfig{}, fmt.Errorf("provider spec: %w", err)
	}
	config, err := endpointintent.NewProviderConfig(ref, spec, pc.BaseURL, pc.CredentialRef, protocolsurface.Kind(pc.ProtocolKind))
	if err != nil {
		return endpointintent.ProviderConfig{}, err
	}
	config, err = config.WithModelID(pc.ModelID)
	if err != nil {
		return endpointintent.ProviderConfig{}, err
	}
	config, err = config.WithTargetAlias(pc.TargetAlias)
	if err != nil {
		return endpointintent.ProviderConfig{}, err
	}
	return config, nil
}

func endpointToSnapshot(ep endpointintent.Endpoint) stateModel.EndpointSnapshot {
	providerConfigs := ep.ProviderConfigs()
	snapshot := stateModel.EndpointSnapshot{
		Name:                      ep.Name().String(),
		SelectedProviderConfigRef: ep.SelectedProviderConfigRef().String(),
		ProviderConfigs:           make([]stateModel.ProviderConfigSnapshot, 0, len(providerConfigs)),
	}
	for _, pc := range providerConfigs {
		snapshot.ProviderConfigs = append(snapshot.ProviderConfigs, stateModel.ProviderConfigSnapshot{
			Ref:           pc.Ref().String(),
			ProviderSpec:  pc.ProviderSpec().String(),
			BaseURL:       pc.BaseURL(),
			CredentialRef: pc.CredentialRef(),
			ModelID:       pc.ModelID(),
			TargetAlias:   pc.TargetAlias(),
			ProtocolKind:  pc.ProtocolKind().String(),
		})
	}
	return snapshot
}

func trafficOperationFamily(ingressFamily string, result string, statusCode int) string {
	switch strings.TrimSpace(strings.ToLower(ingressFamily)) {
	case "responses":
		return "responses"
	case "chat_completions":
		return "chat"
	case "completions":
		return "completions"
	case "messages":
		return "messages"
	}
	if statusCode == 0 {
		return "in flight"
	}
	return "request"
}
