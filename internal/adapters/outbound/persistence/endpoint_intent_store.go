package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

const endpointIntentSchemaVersion = 1

// EndpointIntentStoreConfig selects the one durable JSON file used for v0
// endpoint intent. The schema stays explicit and versioned so refactors do not
// silently mutate operator intent.
type EndpointIntentStoreConfig struct {
	Path string
}

type EndpointIntentStore struct {
	path string
}

type endpointIntentFileDTO struct {
	Version   int           `json:"version"`
	Endpoints []endpointDTO `json:"endpoints"`
}

type endpointDTO struct {
	Name                      string              `json:"name"`
	SelectedProviderConfigRef string              `json:"selected_provider_config_ref"`
	ProviderConfigs           []providerConfigDTO `json:"provider_configs"`
}

type providerConfigDTO struct {
	Ref           string `json:"ref"`
	ProviderSpec  string `json:"provider_spec"`
	BaseURL       string `json:"base_url,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	ModelID       string `json:"model_id,omitempty"`
	SelectedFrame string `json:"selected_frame,omitempty"`
	ProtocolKind  string `json:"protocol_kind,omitempty"`
}

func NewEndpointIntentStore(cfg EndpointIntentStoreConfig) (EndpointIntentStore, error) {
	if cfg.Path == "" {
		return EndpointIntentStore{}, fmt.Errorf("endpoint intent path is required")
	}
	return EndpointIntentStore{path: cfg.Path}, nil
}

func (s EndpointIntentStore) GetEndpoint(ctx context.Context, name endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	endpoints, err := s.ListEndpoints(ctx)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	for _, endpoint := range endpoints {
		if endpoint.Name() == name {
			return endpoint, nil
		}
	}
	return endpointintent.Endpoint{}, fs.ErrNotExist
}

func (s EndpointIntentStore) ListEndpoints(context.Context) ([]endpointintent.Endpoint, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errorsIs(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read endpoint intent: %w", err)
	}
	var dto endpointIntentFileDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, fmt.Errorf("decode endpoint intent: %w", err)
	}
	if dto.Version != endpointIntentSchemaVersion {
		return nil, fmt.Errorf("unsupported endpoint intent schema version %d", dto.Version)
	}
	endpoints := make([]endpointintent.Endpoint, 0, len(dto.Endpoints))
	for _, encoded := range dto.Endpoints {
		endpoint, err := decodeEndpointDTO(encoded)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].Name().String() < endpoints[j].Name().String()
	})
	return endpoints, nil
}

func (s EndpointIntentStore) SaveEndpoints(_ context.Context, endpoints []endpointintent.Endpoint) error {
	encoded := make([]endpointDTO, 0, len(endpoints))
	sorted := slices.Clone(endpoints)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name().String() < sorted[j].Name().String()
	})
	for _, endpoint := range sorted {
		encoded = append(encoded, encodeEndpointDTO(endpoint))
	}
	raw, err := json.MarshalIndent(endpointIntentFileDTO{
		Version:   endpointIntentSchemaVersion,
		Endpoints: encoded,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode endpoint intent: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create endpoint intent directory: %w", err)
	}
	return writeFileAtomically(s.path, raw, 0o644)
}

func decodeEndpointDTO(dto endpointDTO) (endpointintent.Endpoint, error) {
	name, err := endpointintent.ParseEndpointName(dto.Name)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	selectedRef, err := endpointintent.ParseProviderConfigRef(dto.SelectedProviderConfigRef)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	providerConfigs := make([]endpointintent.ProviderConfig, 0, len(dto.ProviderConfigs))
	for _, encoded := range dto.ProviderConfigs {
		ref, err := endpointintent.ParseProviderConfigRef(encoded.Ref)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		spec, err := endpointintent.ParseProviderSpec(encoded.ProviderSpec)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err := endpointintent.NewProviderConfig(
			ref,
			spec,
			encoded.BaseURL,
			encoded.CredentialRef,
			protocolkind.ProtocolKind(encoded.ProtocolKind),
		)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err = providerConfig.WithModelID(encoded.ModelID)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		if encoded.SelectedFrame != "" {
			providerConfig, err = providerConfig.WithSelectedFrame(encoded.SelectedFrame)
			if err != nil {
				return endpointintent.Endpoint{}, err
			}
		}
		providerConfigs = append(providerConfigs, providerConfig)
	}
	endpoint, err := endpointintent.NewEndpoint(name, providerConfigs, selectedRef)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	return endpoint, nil
}

func encodeEndpointDTO(endpoint endpointintent.Endpoint) endpointDTO {
	providerConfigs := endpoint.ProviderConfigs()
	encodedProviderConfigs := make([]providerConfigDTO, 0, len(providerConfigs))
	for _, providerConfig := range providerConfigs {
		encodedProviderConfigs = append(encodedProviderConfigs, providerConfigDTO{
			Ref:           providerConfig.Ref().String(),
			ProviderSpec:  providerConfig.ProviderSpec().String(),
			BaseURL:       providerConfig.BaseURL(),
			CredentialRef: providerConfig.CredentialRef(),
			ModelID:       providerConfig.ModelID(),
			SelectedFrame: providerConfig.SelectedFrame(),
			ProtocolKind:  providerConfig.ProtocolKind().String(),
		})
	}
	return endpointDTO{
		Name:                      endpoint.Name().String(),
		SelectedProviderConfigRef: endpoint.SelectedProviderConfigRef().String(),
		ProviderConfigs:           encodedProviderConfigs,
	}
}

func writeFileAtomically(path string, raw []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp endpoint intent file: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temp endpoint intent file: %w", err)
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temp endpoint intent file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temp endpoint intent file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace endpoint intent file: %w", err)
	}
	return nil
}

func errorsIs(err error, target error) bool {
	return err != nil && target != nil && os.IsNotExist(err)
}
