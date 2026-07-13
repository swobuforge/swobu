package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/ports"
)

func deploymentForModelID(deployments []ports.ProviderDeploymentRecord, modelID string) (ports.ProviderDeploymentRecord, bool) {
	modelID = strings.TrimSpace(modelID) // swobu:io-string source=boundary
	if modelID == "" {
		return ports.ProviderDeploymentRecord{}, false
	}
	for _, deployment := range deployments {
		name := strings.TrimSpace(deployment.Name) // swobu:io-string source=boundary
		if name != "" && name == modelID {
			return deployment, true
		}
		if name == "" && strings.TrimSpace(deployment.ModelName) == modelID { // swobu:io-string source=boundary
			return deployment, true
		}
	}
	return ports.ProviderDeploymentRecord{}, false
}

func deploymentOptionLabel(deployment ports.ProviderDeploymentRecord) string {
	name := strings.TrimSpace(deployment.Name) // swobu:io-string source=boundary
	if name == "" {
		name = strings.TrimSpace(deployment.ModelName) // swobu:io-string source=boundary
	}
	if name == "" {
		return ""
	}
	meta := make([]string, 0, 4)
	if family := strings.TrimSpace(deployment.Family); family != "" { // swobu:io-string source=boundary
		meta = append(meta, family)
	}
	if publisher := strings.TrimSpace(deployment.ModelPublisher); publisher != "" { // swobu:io-string source=boundary
		meta = append(meta, publisher)
	}
	if modelName := strings.TrimSpace(deployment.ModelName); modelName != "" && modelName != name { // swobu:io-string source=boundary
		meta = append(meta, modelName)
	}
	if version := strings.TrimSpace(deployment.ModelVersion); version != "" { // swobu:io-string source=boundary
		meta = append(meta, version)
	}
	if len(meta) == 0 {
		return name
	}
	return name + " (" + strings.Join(meta, " · ") + ")"
}

func deploymentProtocolOptions(deployment ports.ProviderDeploymentRecord, providerSpec string) []string {
	return ports.ResolveProviderDeployment(providerSpec, deployment).ProtocolOptions()
}

func deploymentSelectedProtocol(deployment ports.ProviderDeploymentRecord, providerSpec string, current string) string {
	resolution := ports.ResolveProviderDeployment(providerSpec, deployment)
	current = strings.TrimSpace(current) // swobu:io-string source=boundary
	if current != "" && resolution.SupportsProtocol(current) {
		return current
	}
	return resolution.DefaultProtocol()
}
