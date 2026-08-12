package profile

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

// TestStandardProfilesEnforceOneProfileOwnedConstructionContract proves the
// generic laws once for every ordinary provider. Provider IDs select data here;
// routing remains exhaustive only over durable connection shapes.
func TestStandardProfilesEnforceOneProfileOwnedConstructionContract(t *testing.T) {
	facts := RoutingConstructionFacts()
	for _, entry := range All() {
		if entry.ConnectionShape != routing.ConnectionShapeStandard {
			continue
		}
		provider := string(entry.ProviderID)
		t.Run(provider, func(t *testing.T) {
			valid := routing.ConnectionDraft{Provider: provider, Standard: &routing.StandardConnectionDraft{
				Locator:    validStandardLocator(entry),
				Credential: validStandardCredential(entry),
			}}
			if _, err := routing.FinalizeConnection(valid, facts); err != nil {
				t.Fatalf("valid Standard connection: %v", err)
			}

			switch entry.Credential.Requirement {
			case CredentialRequired:
				missing := valid
				missing.Standard = &routing.StandardConnectionDraft{Locator: valid.Standard.Locator}
				if _, err := routing.FinalizeConnection(missing, facts); err == nil || !strings.Contains(err.Error(), "connection."+provider+".credential") {
					t.Fatalf("missing required credential error = %v", err)
				}
			case CredentialOptional:
				without := valid
				without.Standard = &routing.StandardConnectionDraft{Locator: valid.Standard.Locator}
				if _, err := routing.FinalizeConnection(without, facts); err != nil {
					t.Fatalf("optional credential omitted: %v", err)
				}
			case CredentialUnsupported:
				with := valid
				with.Standard = &routing.StandardConnectionDraft{Locator: valid.Standard.Locator, Credential: "env:UNSUPPORTED_TOKEN"}
				if _, err := routing.FinalizeConnection(with, facts); err == nil || !strings.Contains(err.Error(), "connection."+provider+".credential") {
					t.Fatalf("unsupported credential error = %v", err)
				}
			}

			switch entry.Locator.Kind {
			case LocatorFixed:
				withLocator := valid
				withLocator.Standard = &routing.StandardConnectionDraft{Locator: "https://override.example/v1", Credential: valid.Standard.Credential}
				if _, err := routing.FinalizeConnection(withLocator, facts); err == nil || !strings.Contains(err.Error(), "connection."+provider+".base_url") {
					t.Fatalf("fixed locator override error = %v", err)
				}
			case LocatorBaseURL:
				malformed := valid
				malformed.Standard = &routing.StandardConnectionDraft{Locator: "not a URL", Credential: valid.Standard.Credential}
				if _, err := routing.FinalizeConnection(malformed, facts); err == nil || !strings.Contains(err.Error(), "connection."+provider+".base_url") || strings.Contains(err.Error(), ".locator") {
					t.Fatalf("base URL error = %v", err)
				}
			case LocatorAzureProject:
				malformed := valid
				malformed.Standard = &routing.StandardConnectionDraft{Locator: "https://invalid.example/v1", Credential: valid.Standard.Credential}
				if _, err := routing.FinalizeConnection(malformed, facts); err == nil || !strings.Contains(err.Error(), "connection."+provider+".project_endpoint") || strings.Contains(err.Error(), ".locator") {
					t.Fatalf("project endpoint error = %v", err)
				}
			}
		})
	}
}

func TestLMStudioStandardLocatorRetainsV1Namespace(t *testing.T) {
	_, err := routing.FinalizeConnection(routing.ConnectionDraft{
		Provider: string(ProviderSpecLMStudio),
		Standard: &routing.StandardConnectionDraft{Locator: "http://127.0.0.1:1234/api", Credential: "env:LM_API_TOKEN"},
	}, RoutingConstructionFacts())
	if err == nil || !strings.Contains(err.Error(), "connection.lmstudio.base_url") || !strings.Contains(err.Error(), "/v1") {
		t.Fatalf("LM Studio namespace error = %v", err)
	}
}

func TestCatalogProfileSemanticFieldsFailClosed(t *testing.T) {
	base := Profile{
		ProviderID:      "futurecloud",
		ConnectionShape: routing.ConnectionShapeStandard,
		Credential:      CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference},
	}
	if err := ValidateCatalogProfile(base); err != nil {
		t.Fatalf("valid future profile: %v", err)
	}
	missingShape := base
	missingShape.ConnectionShape = routing.ConnectionShapeInvalid
	if err := ValidateCatalogProfile(missingShape); err == nil {
		t.Fatal("missing connection shape was accepted")
	}
	missingAuthoring := base
	missingAuthoring.Credential.Authoring = CredentialAuthoringInvalid
	if err := ValidateCatalogProfile(missingAuthoring); err == nil {
		t.Fatal("missing credential authoring was accepted")
	}
	for _, test := range []struct {
		name  string
		spec  CredentialSpec
		valid bool
	}{
		{"unsupported without authoring", CredentialSpec{Requirement: CredentialUnsupported, Authoring: CredentialAuthoringNone}, true},
		{"required reference", CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference}, true},
		{"required interactive", CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringInteractive}, true},
		{"optional reference", CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference}, true},
		{"optional ambient or reference", CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringAmbientOrReference}, true},
		{"loopback-sensitive reference", CredentialSpec{Requirement: CredentialRequiredOutsideLoopback, Authoring: CredentialAuthoringReference}, true},
		{"unsupported reference", CredentialSpec{Requirement: CredentialUnsupported, Authoring: CredentialAuthoringReference}, false},
		{"required none", CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringNone}, false},
		{"optional interactive", CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringInteractive}, false},
		{"loopback-sensitive ambient", CredentialSpec{Requirement: CredentialRequiredOutsideLoopback, Authoring: CredentialAuthoringAmbientOrReference}, false},
		{"unknown requirement", CredentialSpec{Requirement: CredentialRequirement(99), Authoring: CredentialAuthoringReference}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := base
			profile.Credential = test.spec
			err := ValidateCatalogProfile(profile)
			if test.valid && err != nil {
				t.Fatalf("valid credential specification: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("incompatible credential specification was accepted")
			}
		})
	}
}

func TestFuturecloudProfileCarriesClosedStandardSemanticsWithoutRoutingMembership(t *testing.T) {
	future := Profile{
		ProviderID:      "futurecloud",
		ConnectionShape: routing.ConnectionShapeStandard,
		Locator:         LocatorSpec{Kind: LocatorFixed, Default: "https://api.futurecloud.example/v1"},
		Credential:      CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference},
	}
	if err := ValidateCatalogProfile(future); err != nil {
		t.Fatalf("future profile validation: %v", err)
	}
	if _, err := validateStandardConnectionForProfile(future, routing.Provider("futurecloud"), routing.StandardConnectionDraft{}); err == nil || !strings.Contains(err.Error(), "connection.futurecloud.credential") {
		t.Fatalf("futurecloud missing credential error = %v", err)
	}
	if _, err := validateStandardConnectionForProfile(future, routing.Provider("futurecloud"), routing.StandardConnectionDraft{Credential: "env:FUTURECLOUD_API_KEY", Locator: "https://override.example/v1"}); err == nil || !strings.Contains(err.Error(), "connection.futurecloud.base_url") {
		t.Fatalf("futurecloud fixed locator error = %v", err)
	}
	if _, err := validateStandardConnectionForProfile(future, routing.Provider("futurecloud"), routing.StandardConnectionDraft{Credential: "env:FUTURECLOUD_API_KEY"}); err != nil {
		t.Fatalf("futurecloud valid Standard path: %v", err)
	}
}

func validStandardLocator(entry Profile) string {
	switch entry.Locator.Kind {
	case LocatorAzureProject:
		return "https://futurecloud.services.ai.azure.com/api/projects/demo"
	default:
		return ""
	}
}

func validStandardCredential(entry Profile) string {
	if entry.Credential.Requirement == CredentialUnsupported {
		return ""
	}
	return "env:TEST_TOKEN"
}
