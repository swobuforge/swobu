// config file contract and endpoint-intent projection together.
package config

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueyaml "cuelang.org/go/encoding/yaml"
	"gopkg.in/yaml.v3"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

//go:embed runtime.cue
var runtimeSchema string

var (
	defaultRuntimeConfigOnce sync.Once
	defaultRuntimeConfig     RuntimeConfig
)

type RuntimeConfig struct {
	BindAddr string
}

type LoadedConfig struct {
	Runtime   RuntimeConfig
	Endpoints []endpointintent.Endpoint
}

type runtimeFileDTO struct {
	BindAddr  string        `json:"bind_addr" yaml:"bind_addr"`
	Endpoints []endpointDTO `json:"endpoints" yaml:"endpoints"`
}

type endpointDTO struct {
	Name                      string              `json:"name" yaml:"name"`
	SelectedProviderConfigRef string              `json:"selected_provider_config_ref" yaml:"selected_provider_config_ref"`
	ProviderConfigs           []providerConfigDTO `json:"provider_configs" yaml:"provider_configs"`
}

type providerConfigDTO struct {
	Ref           string `json:"ref" yaml:"ref"`
	ProviderSpec  string `json:"provider_spec" yaml:"provider_spec"`
	BaseURL       string `json:"base_url" yaml:"base_url"`
	CredentialRef string `json:"credential_ref" yaml:"credential_ref"`
	ModelID       string `json:"model_id,omitempty" yaml:"model_id,omitempty"`
	TargetAlias   string `json:"target_alias,omitempty" yaml:"target_alias,omitempty"`
	ProtocolKind  string `json:"protocol_kind" yaml:"protocol_kind"`
}

func (c RuntimeConfig) WithDefaults() RuntimeConfig {
	out := c
	if strings.TrimSpace(out.BindAddr) == "" {
		out.BindAddr = DefaultBindAddr()
	}
	return out
}

// DefaultBindAddr derives the daemon bind default from the CUE runtime schema
// so Go-side callers do not carry a second hard-coded copy of the same value.
func DefaultBindAddr() string {
	return defaultRuntimeConfigValue().BindAddr
}

func DefaultDaemonURL() string {
	if daemonURL := strings.TrimSpace(os.Getenv("SWOBU_DAEMON_URL")); daemonURL != "" {
		return daemonURL
	}
	return "http://" + DefaultBindAddr()
}

func DefaultConfigPath() string {
	if configPath := strings.TrimSpace(os.Getenv("SWOBU_CONFIG_PATH")); configPath != "" {
		return configPath
	}
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return "swobu.yaml"
	}
	return filepath.Join(configDir, "swobu", "swobu.yaml")
}

// EnsureDefaultConfigFile guarantees there is a writable runtime config at the
// default config location used by attach-or-start launcher flows.
func EnsureDefaultConfigFile() (string, error) {
	configPath := DefaultConfigPath()
	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat default config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	runtime := RuntimeConfig{BindAddr: defaultBindAddrForDaemonURL()}
	if err := Save(configPath, runtime.WithDefaults(), nil); err != nil {
		return "", fmt.Errorf("initialize default config: %w", err)
	}
	return configPath, nil
}

func defaultBindAddrForDaemonURL() string {
	parsedURL, err := url.Parse(DefaultDaemonURL())
	if err != nil || strings.TrimSpace(parsedURL.Host) == "" {
		return DefaultBindAddr()
	}
	return parsedURL.Host
}

func (c RuntimeConfig) Validate() error {
	if strings.TrimSpace(c.BindAddr) == "" {
		return fmt.Errorf("bind address is required")
	}
	return nil
}

func Load(path string) (LoadedConfig, error) {
	if strings.TrimSpace(path) == "" {
		return LoadedConfig{}, fmt.Errorf("config path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return LoadedConfig{}, fmt.Errorf("read config: %w", err)
	}

	ctx := cuecontext.New()
	schema := ctx.CompileString(runtimeSchema)
	if err := schema.Err(); err != nil {
		return LoadedConfig{}, fmt.Errorf("compile runtime schema: %w", err)
	}

	data, err := compileConfigData(ctx, path, raw)
	if err != nil {
		return LoadedConfig{}, err
	}
	// Unifying schema and input applies CUE defaults and constraints before the
	// result is decoded into the smaller Go DTOs below.
	value := schema.Unify(data)
	if err := value.Validate(cue.Concrete(true)); err != nil {
		return LoadedConfig{}, fmt.Errorf("validate config: %w", err)
	}

	var dto runtimeFileDTO
	if err := value.Decode(&dto); err != nil {
		return LoadedConfig{}, fmt.Errorf("decode config: %w", err)
	}

	loaded := LoadedConfig{
		Runtime: RuntimeConfig{
			BindAddr: dto.BindAddr,
		}.WithDefaults(),
		Endpoints: make([]endpointintent.Endpoint, 0, len(dto.Endpoints)),
	}
	if err := loaded.Runtime.Validate(); err != nil {
		return LoadedConfig{}, err
	}

	for _, encoded := range dto.Endpoints {
		endpoint, err := decodeEndpointDTO(encoded)
		if err != nil {
			return LoadedConfig{}, err
		}
		loaded.Endpoints = append(loaded.Endpoints, endpoint)
	}

	return loaded, nil
}

func Save(path string, runtime RuntimeConfig, endpoints []endpointintent.Endpoint) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("config path is required")
	}
	runtime = runtime.WithDefaults()
	if err := runtime.Validate(); err != nil {
		return err
	}

	dto := runtimeFileDTO{
		BindAddr:  runtime.BindAddr,
		Endpoints: make([]endpointDTO, 0, len(endpoints)),
	}
	for _, endpoint := range endpoints {
		dto.Endpoints = append(dto.Endpoints, encodeEndpointDTO(endpoint))
	}

	raw, err := marshalConfigData(path, dto)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func compileConfigData(ctx *cue.Context, path string, raw []byte) (cue.Value, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		file, err := cueyaml.Extract(path, raw)
		if err != nil {
			return cue.Value{}, fmt.Errorf("extract yaml config: %w", err)
		}
		value := ctx.BuildFile(file)
		if err := value.Err(); err != nil {
			return cue.Value{}, fmt.Errorf("build yaml config: %w", err)
		}
		return value, nil
	case ".json":
		value := ctx.CompileBytes(raw)
		if err := value.Err(); err != nil {
			return cue.Value{}, fmt.Errorf("compile json config: %w", err)
		}
		return value, nil
	default:
		return cue.Value{}, fmt.Errorf("unsupported config extension %q", filepath.Ext(path))
	}
}

func marshalConfigData(path string, dto runtimeFileDTO) ([]byte, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		raw, err := yaml.Marshal(dto)
		if err != nil {
			return nil, fmt.Errorf("encode yaml config: %w", err)
		}
		return raw, nil
	case ".json":
		raw, err := json.MarshalIndent(dto, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode json config: %w", err)
		}
		return append(raw, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported config extension %q", filepath.Ext(path))
	}
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
			protocolsurface.Kind(encoded.ProtocolKind),
		)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err = providerConfig.WithModelID(encoded.ModelID)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err = providerConfig.WithTargetAlias(encoded.TargetAlias)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfigs = append(providerConfigs, providerConfig)
	}
	return endpointintent.NewEndpoint(name, providerConfigs, selectedRef)
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
			TargetAlias:   providerConfig.TargetAlias(),
			ProtocolKind:  providerConfig.ProtocolKind().String(),
		})
	}
	return endpointDTO{
		Name:                      endpoint.Name().String(),
		SelectedProviderConfigRef: endpoint.SelectedProviderConfigRef().String(),
		ProviderConfigs:           encodedProviderConfigs,
	}
}

func defaultRuntimeConfigValue() RuntimeConfig {
	defaultRuntimeConfigOnce.Do(func() {
		ctx := cuecontext.New()
		schema := ctx.CompileString(runtimeSchema)
		if err := schema.Err(); err != nil {
			panic(fmt.Sprintf("compile runtime schema defaults: %v", err))
		}
		value := schema
		if err := value.Validate(cue.Concrete(true)); err != nil {
			panic(fmt.Sprintf("validate runtime schema defaults: %v", err))
		}
		var dto runtimeFileDTO
		if err := value.Decode(&dto); err != nil {
			panic(fmt.Sprintf("decode runtime schema defaults: %v", err))
		}
		defaultRuntimeConfig = RuntimeConfig{
			BindAddr: dto.BindAddr,
		}
	})
	return defaultRuntimeConfig
}
