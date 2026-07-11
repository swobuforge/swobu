package httpapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

type endpointAutoProtocolResolver struct {
	probe endpointAutoProtocolProbeFunc
}

func newEndpointAutoProtocolResolver(probe endpointAutoProtocolProbeFunc) endpointAutoProtocolResolver {
	return endpointAutoProtocolResolver{probe: probe}
}

func (r endpointAutoProtocolResolver) Resolve(ctx context.Context, endpoint endpointintent.Endpoint, doc endpointDocument) (endpointintent.Endpoint, error) {
	if r.probe == nil {
		return endpoint, nil
	}
	rawByRef := mapRawProviderProtocolsByRef(doc)
	configs := endpoint.ProviderConfigs()
	selectedRef := endpoint.SelectedProviderConfigRef()
	rebuilt := false
	for i := range configs {
		rawProtocol := rawByRef[strings.TrimSpace(configs[i].Ref().String())] // swobu:io-string source=boundary
		if rawProtocol != "" && rawProtocol != profile.ProviderProtocolAuto {
			continue
		}
		resolved, err := r.resolveOne(ctx, endpoint.Name(), configs, i, selectedRef)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		configs[i], err = configs[i].WithProviderProtocol(resolved)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		rebuilt = true
	}
	if !rebuilt {
		return endpoint, nil
	}
	return endpointintent.NewEndpoint(endpoint.Name(), configs, selectedRef)
}

func mapRawProviderProtocolsByRef(doc endpointDocument) map[string]string {
	out := make(map[string]string, len(doc.ProviderConfigs))
	for _, cfg := range doc.ProviderConfigs {
		out[strings.TrimSpace(cfg.Ref)] = strings.TrimSpace(cfg.ProviderProtocol) // swobu:io-string source=boundary
	}
	return out
}

func (r endpointAutoProtocolResolver) resolveOne(
	ctx context.Context,
	endpointName endpointintent.EndpointName,
	configs []endpointintent.ProviderConfig,
	targetIdx int,
	selectedRef endpointintent.ProviderConfigRef,
) (string, error) {
	cfg := configs[targetIdx]
	modelID := strings.TrimSpace(cfg.ModelID()) // swobu:io-string source=boundary
	if modelID == "" {
		return "", fmt.Errorf("provider protocol auto requires model_id for provider ref %q", cfg.Ref().String())
	}
	var failures []string
	for _, variant := range profile.ConcreteProviderProtocolsForSpec(cfg.ProviderSpec().String()) {
		candidateCfg, err := cfg.WithProviderProtocol(variant)
		if err != nil {
			failures = append(failures, variant+": "+err.Error())
			continue
		}
		candidateConfigs := append([]endpointintent.ProviderConfig(nil), configs...)
		candidateConfigs[targetIdx] = candidateCfg
		candidateSelectedRef := selectedRef
		if strings.TrimSpace(selectedRef.String()) != strings.TrimSpace(cfg.Ref().String()) { // swobu:io-string source=boundary
			candidateSelectedRef = cfg.Ref()
		}
		candidateEndpoint, err := endpointintent.NewEndpoint(endpointName, candidateConfigs, candidateSelectedRef)
		if err != nil {
			failures = append(failures, variant+": "+err.Error())
			continue
		}
		if err := r.probeVariant(ctx, endpointName, modelID, candidateEndpoint); err == nil {
			return variant, nil
		} else {
			failures = append(failures, variant+": "+strings.TrimSpace(err.Error())) // swobu:io-string source=boundary
		}
	}
	if len(failures) == 0 {
		return "", fmt.Errorf("provider %q has no concrete protocols to resolve auto", cfg.ProviderSpec().String())
	}
	return "", fmt.Errorf("provider protocol auto could not be resolved for provider ref %q: %s", cfg.Ref().String(), strings.Join(failures, "; "))
}

func (r endpointAutoProtocolResolver) probeVariant(
	ctx context.Context,
	endpointName endpointintent.EndpointName,
	modelID string,
	candidate endpointintent.Endpoint,
) error {
	ping := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: modelID,
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "ping")},
	})
	attemptCtx, cancel := context.WithTimeout(ctx, autoProtocolProbeAttemptTimeout)
	defer cancel()
	out, err := r.probe(attemptCtx, candidate, exchange.HandleInput{
		EndpointName: endpointName,
		ClientFamily: canonical.IngressFamilyResponses,
		Request:      ping,
		Contract:     exchange.NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		return err
	}
	_ = exchange.CloseProviderResponseStream(out.Response)
	return nil
}
