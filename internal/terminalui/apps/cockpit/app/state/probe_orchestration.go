package state

import "strings"

type routingProbeIdentity struct {
	Scope         string
	ProviderSpec  string
	BaseURL       string
	AuthHeader    string
	CredentialRef string
}

func newRoutingProbeIdentity(scope, providerSpec, baseURL, authHeader, credentialRef string) routingProbeIdentity {
	return routingProbeIdentity{
		Scope:         strings.TrimSpace(scope),         // swobu:io-string source=boundary
		ProviderSpec:  strings.TrimSpace(providerSpec),  // swobu:io-string source=boundary
		BaseURL:       strings.TrimSpace(baseURL),       // swobu:io-string source=boundary
		AuthHeader:    strings.TrimSpace(authHeader),    // swobu:io-string source=boundary
		CredentialRef: strings.TrimSpace(credentialRef), // swobu:io-string source=boundary
	}
}

func (id routingProbeIdentity) matchesCreateDraft(model *Model) bool {
	return strings.TrimSpace(model.CreateDraftModelProviderSpec) == id.ProviderSpec && // swobu:io-string source=boundary
		strings.TrimSpace(model.CreateDraftModelBaseURL) == id.BaseURL && // swobu:io-string source=boundary
		strings.TrimSpace(model.CreateDraftModelAuthHeader) == id.AuthHeader && // swobu:io-string source=boundary
		strings.TrimSpace(model.CreateDraftModelCredentialRef) == id.CredentialRef // swobu:io-string source=boundary
}

func (id routingProbeIdentity) matchesAddModelDraft(model *Model) bool {
	return strings.TrimSpace(model.AddModelDraftProviderSpec) == id.ProviderSpec && // swobu:io-string source=boundary
		strings.TrimSpace(model.AddModelDraftBaseURL) == id.BaseURL && // swobu:io-string source=boundary
		strings.TrimSpace(model.AddModelDraftAuthHeader) == id.AuthHeader && // swobu:io-string source=boundary
		strings.TrimSpace(model.AddModelDraftCredentialRef) == id.CredentialRef // swobu:io-string source=boundary
}

func primeCreateDraftModelCatalogProbe(model *Model, id routingProbeIdentity) {
	model.CreateDraftModelProviderSpec = id.ProviderSpec
	model.CreateDraftModelBaseURL = id.BaseURL
	model.CreateDraftModelAuthHeader = id.AuthHeader
	model.CreateDraftModelCredentialRef = id.CredentialRef
	model.CreateDraftModelDeployments = nil
	model.CreateDraftModelError = ""
	model.CreateDraftModelProbePending = true
	model.CreateDraftModelTestProtocol = ""
	model.CreateDraftModelTestPassed = false
}

func clearCreateDraftModelCatalogProbe(model *Model) {
	model.CreateDraftModelDeployments = nil
	model.CreateDraftModelError = ""
	model.CreateDraftModelProbePending = false
	model.CreateDraftModelProviderSpec = ""
	model.CreateDraftModelAuthHeader = ""
	model.CreateDraftModelBaseURL = ""
	model.CreateDraftModelCredentialRef = ""
	model.CreateDraftModelTestProtocol = ""
	model.CreateDraftModelTestPassed = false
}

func primeAddModelCatalogProbe(model *Model, id routingProbeIdentity, providerProtocol string) {
	model.AddModelDraftProviderSpec = id.ProviderSpec
	model.AddModelDraftProviderProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	model.AddModelDraftBaseURL = id.BaseURL
	model.AddModelDraftAuthHeader = id.AuthHeader
	model.AddModelDraftCredentialRef = id.CredentialRef
	model.AddModelDraftModelDeployments = nil
	model.AddModelDraftModelError = ""
	model.AddModelDraftModelProbePending = true
}

func clearAddModelCatalogProbe(model *Model) {
	model.AddModelDraftModelDeployments = nil
	model.AddModelDraftModelError = ""
	model.AddModelDraftModelProbePending = false
	model.AddModelDraftProviderSpec = ""
	model.AddModelDraftProviderProtocol = ""
	model.AddModelDraftBaseURL = ""
	model.AddModelDraftAuthHeader = ""
	model.AddModelDraftCredentialRef = ""
}
