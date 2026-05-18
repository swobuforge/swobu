package operatorclient

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

// endpointDocument mirrors the HTTP wire format for a single endpoint.
type endpointDocument struct {
	Name                      string                   `json:"name"`
	SelectedProviderConfigRef string                   `json:"selected_provider_config_ref"`
	ProviderConfigs           []providerConfigDocument `json:"provider_configs"`
}

type providerConfigDocument struct {
	Ref           string `json:"ref"`
	ProviderSpec  string `json:"provider_spec"`
	BaseURL       string `json:"base_url,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	ModelID       string `json:"model_id,omitempty"`
	TargetAlias   string `json:"target_alias,omitempty"`
	SelectedFrame string `json:"selected_frame,omitempty"`
	ProtocolKind  string `json:"protocol_kind,omitempty"`
}

type endpointListDocument struct {
	Endpoints []endpointDocument `json:"endpoints"`
}

func endpointDocumentFromDomain(ep endpointintent.Endpoint) endpointDocument {
	providerConfigs := ep.ProviderConfigs()
	doc := endpointDocument{
		Name:                      ep.Name().String(),
		SelectedProviderConfigRef: ep.SelectedProviderConfigRef().String(),
		ProviderConfigs:           make([]providerConfigDocument, 0, len(providerConfigs)),
	}
	for _, pc := range providerConfigs {
		doc.ProviderConfigs = append(doc.ProviderConfigs, providerConfigDocument{
			Ref:           pc.Ref().String(),
			ProviderSpec:  pc.ProviderSpec().String(),
			BaseURL:       pc.BaseURL(),
			CredentialRef: pc.CredentialRef(),
			ModelID:       pc.ModelID(),
			TargetAlias:   pc.TargetAlias(),
			SelectedFrame: pc.SelectedFrame(),
			ProtocolKind:  pc.ProtocolKind().String(),
		})
	}
	return doc
}

func (d endpointDocument) toDomain() (endpointintent.Endpoint, error) {
	name, err := endpointintent.ParseEndpointName(d.Name)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	selectedRef, err := endpointintent.ParseProviderConfigRef(d.SelectedProviderConfigRef)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	providerConfigs := make([]endpointintent.ProviderConfig, 0, len(d.ProviderConfigs))
	for _, pc := range d.ProviderConfigs {
		ref, err := endpointintent.ParseProviderConfigRef(pc.Ref)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		spec, err := endpointintent.ParseProviderSpec(pc.ProviderSpec)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		config, err := endpointintent.NewProviderConfig(ref, spec, pc.BaseURL, pc.CredentialRef)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		if strings.TrimSpace(pc.SelectedFrame) != "" { // swobu:io-string source=boundary
			config, err = config.WithSelectedFrame(pc.SelectedFrame)
			if err != nil {
				return endpointintent.Endpoint{}, err
			}
		}
		config, err = config.WithModelID(pc.ModelID)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		config, err = config.WithTargetAlias(pc.TargetAlias)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfigs = append(providerConfigs, config)
	}
	return endpointintent.NewEndpoint(name, providerConfigs, selectedRef)
}
