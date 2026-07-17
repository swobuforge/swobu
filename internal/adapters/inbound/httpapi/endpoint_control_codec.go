package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	operatorendpoints "github.com/swobuforge/swobu/internal/app/operator/endpoints"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/profile"
)

func encodeEndpointDocument(endpoint endpointintent.Endpoint) endpointDocument {
	providerConfigs := endpoint.ProviderConfigs()
	doc := endpointDocument{
		Name:                      endpoint.Name().String(),
		SelectedProviderConfigRef: endpoint.SelectedProviderConfigRef().String(),
		ProviderConfigs:           make([]providerConfigDocument, 0, len(providerConfigs)),
	}
	for _, providerConfig := range providerConfigs {
		providerProtocol := profile.EncodeProviderProtocolForPersistence(providerConfig.ProviderProtocol())
		var tr, tw *int
		if providerConfig.TargetRank() > 1 {
			v := providerConfig.TargetRank()
			tr = &v
		}
		if providerConfig.TargetWeight() > 1 {
			v := providerConfig.TargetWeight()
			tw = &v
		}
		doc.ProviderConfigs = append(doc.ProviderConfigs, providerConfigDocument{
			Ref:              providerConfig.Ref().String(),
			ProviderSpec:     providerConfig.ProviderSpec().String(),
			BaseURL:          providerConfig.BaseURL(),
			AuthMode:         providerConfig.AuthMode(),
			AuthHeader:       providerConfig.AuthHeader(),
			CredentialRef:    providerConfig.CredentialRef(),
			RouteModelID:     providerConfig.RouteModelID(),
			ModelID:          providerConfig.ModelID(),
			TargetAlias:      providerConfig.TargetAlias(),
			TargetRank:       tr,
			TargetWeight:     tw,
			ProviderProtocol: providerProtocol,
		})
	}
	return doc
}

func decodeEndpointDocument(doc endpointDocument) (endpointintent.Endpoint, error) {
	name, err := endpointintent.ParseEndpointName(doc.Name)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	selectedRef, err := endpointintent.ParseProviderConfigRef(doc.SelectedProviderConfigRef)
	if err != nil {
		return endpointintent.Endpoint{}, err
	}
	providerConfigs := make([]endpointintent.ProviderConfig, 0, len(doc.ProviderConfigs))
	for _, encoded := range doc.ProviderConfigs {
		ref, err := endpointintent.ParseProviderConfigRef(encoded.Ref)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		spec, err := endpointintent.ParseProviderSpec(encoded.ProviderSpec)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err := endpointintent.NewProviderConfig(ref, spec, encoded.BaseURL, encoded.CredentialRef)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err = providerConfig.WithAuthMode(encoded.AuthMode)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		providerConfig, err = providerConfig.WithAuthHeader(encoded.AuthHeader)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		decodedProtocolInput := strings.TrimSpace(encoded.ProviderProtocol) // swobu:io-string source=boundary
		if decodedProtocolInput == profile.ProviderProtocolAuto {
			decodedProtocolInput = ""
		}
		providerProtocol, err := profile.DecodeProviderProtocolFromPersistence(spec.String(), decodedProtocolInput)
		if err != nil {
			return endpointintent.Endpoint{}, err
		}
		if providerProtocol != "" {
			providerConfig, err = providerConfig.WithProviderProtocol(providerProtocol)
			if err != nil {
				return endpointintent.Endpoint{}, err
			}
		}
		providerConfig, err = providerConfig.WithRouteModelID(encoded.RouteModelID)
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
		if encoded.TargetRank != nil {
			providerConfig, err = providerConfig.WithTargetRank(*encoded.TargetRank)
			if err != nil {
				return endpointintent.Endpoint{}, err
			}
		}
		if encoded.TargetWeight != nil {
			providerConfig, err = providerConfig.WithTargetWeight(*encoded.TargetWeight)
			if err != nil {
				return endpointintent.Endpoint{}, err
			}
		}
		providerConfigs = append(providerConfigs, providerConfig)
	}
	return endpointintent.NewEndpoint(name, providerConfigs, selectedRef)
}

func resolveAutoProviderProtocols(ctx context.Context, endpoint endpointintent.Endpoint, doc endpointDocument, probe endpointAutoProtocolProbeFunc) (endpointintent.Endpoint, error) {
	return newEndpointAutoProtocolResolver(probe).Resolve(ctx, endpoint, doc)
}

func writeEndpointControlJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeEndpointControlError(w http.ResponseWriter, err error) {
	var commandErr operatorendpoints.CommandError
	if !errors.As(err, &commandErr) {
		commandErr = operatorendpoints.CommandError{
			Code:    operatorendpoints.CommandInternal,
			Message: "endpoint control plane failed",
			Err:     err,
		}
	}
	writeEndpointControlJSON(w, statusCodeForEndpointControlError(commandErr.Code), endpointControlErrorResponse{
		Error: endpointControlErrorBody{
			Code:    string(commandErr.Code),
			Message: commandErr.Error(),
		},
	})
}

func statusCodeForEndpointControlError(code operatorendpoints.CommandErrorCode) int {
	switch code {
	case operatorendpoints.CommandInvalidArgument:
		return http.StatusBadRequest
	case operatorendpoints.CommandNotFound:
		return http.StatusNotFound
	case operatorendpoints.CommandConflict:
		return http.StatusConflict
	case operatorendpoints.CommandUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
