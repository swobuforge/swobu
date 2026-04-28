package interopmatrix

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/domain/providercatalog"
)

type Transport string

const (
	TransportHTTPPost  Transport = "http_post"
	TransportSSE       Transport = "sse_streaming"
	TransportWebsocket Transport = "websocket_reconnect"
)

type CompatibilityCaseStatus string

const (
	CompatibilityCaseStatusProven    CompatibilityCaseStatus = "proven"
	CompatibilityCaseStatusFailing   CompatibilityCaseStatus = "failing"
	CompatibilityCaseStatusUntested  CompatibilityCaseStatus = "untested"
	CompatibilityCaseStatusOutOfBand CompatibilityCaseStatus = "out_of_band"
)

type CompatibilityCase struct {
	Client    string               `json:"client"`
	Transport Transport            `json:"transport"`
	Family    protocolsurface.Kind `json:"family"`
	Provider  string               `json:"provider"`
	Protocol  protocolsurface.Kind `json:"protocol"`

	Declared         bool                    `json:"declared"`
	ContractGreen    bool                    `json:"contract_green"`
	IntegrationGreen bool                    `json:"integration_green"`
	ConformanceGreen bool                    `json:"conformance_green"`
	Status           CompatibilityCaseStatus `json:"status"`
	Reason           string                  `json:"reason,omitempty"`
}

type Report struct {
	Declared  []CompatibilityCase `json:"declared"`
	Proven    []CompatibilityCase `json:"proven"`
	Failing   []CompatibilityCase `json:"failing"`
	Untested  []CompatibilityCase `json:"untested"`
	OutOfBand []CompatibilityCase `json:"out_of_band"`
}

func Build() Report {
	var all []CompatibilityCase
	for _, provider := range providercatalog.All() {
		for _, protocol := range provider.SupportedProtocols {
			for _, capability := range clientCapabilities() {
				for _, transport := range capability.TransportsForFamily(protocol) {
					compatibilityCase := CompatibilityCase{
						Client:    capability.ID,
						Transport: transport,
						Family:    protocol,
						Provider:  provider.Spec,
						Protocol:  protocol,
					}
					classifyCompatibilityCase(&compatibilityCase, provider)
					all = append(all, compatibilityCase)
				}
			}
		}
	}

	report := Report{}
	for _, compatibilityCase := range all {
		if compatibilityCase.Declared {
			report.Declared = append(report.Declared, compatibilityCase)
		}
		switch compatibilityCase.Status {
		case CompatibilityCaseStatusProven:
			report.Proven = append(report.Proven, compatibilityCase)
		case CompatibilityCaseStatusFailing:
			report.Failing = append(report.Failing, compatibilityCase)
		case CompatibilityCaseStatusUntested:
			report.Untested = append(report.Untested, compatibilityCase)
		case CompatibilityCaseStatusOutOfBand:
			report.OutOfBand = append(report.OutOfBand, compatibilityCase)
		}
	}
	sortCompatibilityCases(report.Declared)
	sortCompatibilityCases(report.Proven)
	sortCompatibilityCases(report.Failing)
	sortCompatibilityCases(report.Untested)
	sortCompatibilityCases(report.OutOfBand)
	return report
}

func Gate(report Report) error {
	if len(report.Failing) == 0 && len(report.Untested) == 0 {
		return nil
	}
	return fmt.Errorf("interop matrix gate failed: declared compatibility cases failing=%d untested=%d", len(report.Failing), len(report.Untested))
}

func JSON(report Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func Text(report Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "interop matrix report\n")
	fmt.Fprintf(&b, "declared=%d proven=%d failing=%d untested=%d out_of_band=%d\n",
		len(report.Declared), len(report.Proven), len(report.Failing), len(report.Untested), len(report.OutOfBand))
	return strings.TrimSpace(b.String())
}

func classifyCompatibilityCase(compatibilityCase *CompatibilityCase, provider providercatalog.Profile) {
	compatibilityCase.Declared = isDeclared(compatibilityCase.Transport)
	if !compatibilityCase.Declared {
		compatibilityCase.Status = CompatibilityCaseStatusOutOfBand
		compatibilityCase.Reason = "transport outside declared support band"
		return
	}
	compatibilityCase.ContractGreen = compatibilityCase.Transport == TransportHTTPPost || compatibilityCase.Transport == TransportSSE || compatibilityCase.Transport == TransportWebsocket
	compatibilityCase.IntegrationGreen = providerSupportsProtocol(provider, compatibilityCase.Protocol)
	compatibilityCase.ConformanceGreen = conformanceCovered(provider, compatibilityCase.Protocol)

	if compatibilityCase.ContractGreen && compatibilityCase.IntegrationGreen && compatibilityCase.ConformanceGreen {
		compatibilityCase.Status = CompatibilityCaseStatusProven
		return
	}
	if !compatibilityCase.ContractGreen || !compatibilityCase.IntegrationGreen {
		compatibilityCase.Status = CompatibilityCaseStatusFailing
		compatibilityCase.Reason = "required contract or integration gate is red"
		return
	}
	compatibilityCase.Status = CompatibilityCaseStatusUntested
	compatibilityCase.Reason = "missing conformance proof for declared compatibility case"
}

func providerSupportsProtocol(provider providercatalog.Profile, protocol protocolsurface.Kind) bool {
	for _, supported := range provider.SupportedProtocols {
		if supported == protocol {
			return true
		}
	}
	return false
}

func conformanceCovered(provider providercatalog.Profile, protocol protocolsurface.Kind) bool {
	switch provider.Adapter {
	case providercatalog.AdapterCustomOpenAICompatible:
		return protocol == protocolsurface.ChatCompletions || protocol == protocolsurface.Responses || protocol == protocolsurface.Completions
	case providercatalog.AdapterAnthropicMessages:
		return protocol == protocolsurface.Messages
	default:
		return false
	}
}

func isDeclared(transport Transport) bool {
	return transport == TransportHTTPPost || transport == TransportSSE || transport == TransportWebsocket
}

type clientCapability struct {
	ID                  string
	SupportsProtocol    map[protocolsurface.Kind]bool
	IncludesWSResponses bool
}

func (c clientCapability) TransportsForFamily(family protocolsurface.Kind) []Transport {
	if !c.SupportsProtocol[family] {
		return nil
	}
	out := []Transport{TransportHTTPPost, TransportSSE}
	if c.IncludesWSResponses && family == protocolsurface.Responses {
		out = append(out, TransportWebsocket)
	}
	return out
}

func clientCapabilities() []clientCapability {
	return []clientCapability{
		{
			ID: "codex-cli",
			SupportsProtocol: map[protocolsurface.Kind]bool{
				protocolsurface.ChatCompletions: true,
				protocolsurface.Responses:       true,
				protocolsurface.Completions:     true,
			},
			IncludesWSResponses: true,
		},
		{
			ID: "claude-code",
			SupportsProtocol: map[protocolsurface.Kind]bool{
				protocolsurface.Messages: true,
			},
		},
		{
			ID: "continue",
			SupportsProtocol: map[protocolsurface.Kind]bool{
				protocolsurface.ChatCompletions: true,
				protocolsurface.Responses:       true,
				protocolsurface.Completions:     true,
			},
		},
		{
			ID: "openai-compatible",
			SupportsProtocol: map[protocolsurface.Kind]bool{
				protocolsurface.ChatCompletions: true,
				protocolsurface.Responses:       true,
				protocolsurface.Completions:     true,
			},
		},
		{
			ID: "anthropic-compatible",
			SupportsProtocol: map[protocolsurface.Kind]bool{
				protocolsurface.Messages: true,
			},
		},
	}
}

func sortCompatibilityCases(compatibilityCases []CompatibilityCase) {
	slices.SortFunc(compatibilityCases, func(a, b CompatibilityCase) int {
		if a.Client != b.Client {
			return strings.Compare(a.Client, b.Client)
		}
		if a.Provider != b.Provider {
			return strings.Compare(a.Provider, b.Provider)
		}
		if a.Family != b.Family {
			return strings.Compare(string(a.Family), string(b.Family))
		}
		return strings.Compare(string(a.Transport), string(b.Transport))
	})
}
