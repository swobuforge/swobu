package effect

import (
	"fmt"
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
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
	baseURL := pc.BaseURL
	if strings.EqualFold(strings.TrimSpace(pc.ProviderSpec), "bedrock") { // swobu:io-string source=boundary
		if derived := stateModel.BedrockBaseURLForRegion(pc.Region); strings.TrimSpace(derived) != "" { // swobu:io-string source=boundary
			baseURL = derived
		} else if strings.TrimSpace(baseURL) == "" {
			region := strings.TrimSpace(os.Getenv("AWS_REGION")) // swobu:io-string source=boundary
			if region == "" {
				region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION")) // swobu:io-string source=boundary
			}
			if region != "" {
				baseURL = stateModel.BedrockBaseURLForRegion(region)
			}
		}
	}
	config, err := endpointintent.NewProviderConfig(ref, spec, baseURL, pc.CredentialRef)
	if err != nil {
		return endpointintent.ProviderConfig{}, err
	}
	config, err = config.WithAuthHeader(pc.AuthHeader)
	if err != nil {
		return endpointintent.ProviderConfig{}, err
	}
	providerProtocol := strings.TrimSpace(pc.ProviderProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == "auto" {
		// Preserve unresolved auto at the save boundary so the daemon-owned
		// model probe can choose the concrete protocol from live catalog data.
		config = config.WithProviderProtocolAuto()
	} else {
		config, err = config.WithProviderProtocol(providerProtocol)
		if err != nil {
			return endpointintent.ProviderConfig{}, err
		}
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
			Ref:              pc.Ref().String(),
			ProviderSpec:     pc.ProviderSpec().String(),
			Region:           stateModel.BedrockRegionFromBaseURL(pc.BaseURL()),
			BaseURL:          pc.BaseURL(),
			AuthHeader:       pc.AuthHeader(),
			CredentialRef:    pc.CredentialRef(),
			ModelID:          pc.ModelID(),
			TargetAlias:      pc.TargetAlias(),
			ProviderProtocol: pc.ProviderProtocol(),
		})
	}
	return snapshot
}

func trafficOperationFamily(clientFamily string, result string, statusCode int) string {
	family := strings.TrimSpace(strings.ToLower(clientFamily)) // swobu:io-string source=boundary
	if family == "responses" {
		return "responses"
	}
	if family == "chat_completions" {
		return "chat"
	}
	if family == "completions" {
		return "completions"
	}
	if family == "messages" {
		return "messages"
	}
	if statusCode == 0 {
		return "in flight"
	}
	return "request"
}
