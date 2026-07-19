package compat_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

type scopedFeature struct {
	Feature  compat.Feature
	Base     compat.Feature
	Protocol protocolkind.ProtocolKind
	Provider profile.ProviderID
}

// scopedFeatures declares every production feature whose final segments are
// representation scope rather than canonical path. The relationship is
// explicit because it cannot be recovered truthfully from final strings.
var scopedFeatures = []scopedFeature{
	{
		Feature:  compat.RequestPreviousResponseResponses,
		Base:     compat.RequestPreviousResponse,
		Protocol: protocolkind.Responses,
	},
	{
		Feature:  compat.ResponseIDResponses,
		Base:     compat.ResponseID,
		Protocol: protocolkind.Responses,
	},
}

var featureSegment = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func TestFeatureVocabularyHasValidRootedPathsAndDeclaredScopes(t *testing.T) {
	allFeatures := productionFeatureConstants(t)
	seen := map[compat.Feature]struct{}{}
	for _, feature := range allFeatures {
		if _, duplicate := seen[feature]; duplicate {
			t.Fatalf("duplicate feature %q", feature)
		}
		seen[feature] = struct{}{}
		if err := validateFeaturePath(feature); err != nil {
			t.Fatalf("feature %q: %v", feature, err)
		}
	}

	seenScoped := map[compat.Feature]struct{}{}
	for _, scoped := range scopedFeatures {
		if _, duplicate := seenScoped[scoped.Feature]; duplicate {
			t.Fatalf("duplicate scoped feature metadata for %q", scoped.Feature)
		}
		seenScoped[scoped.Feature] = struct{}{}
		if _, enumerated := seen[scoped.Feature]; !enumerated {
			t.Fatalf("scoped feature %q is absent from the production vocabulary", scoped.Feature)
		}
		if err := validateScopedFeature(scoped); err != nil {
			t.Fatalf("scoped feature %q: %v", scoped.Feature, err)
		}
	}
	for _, feature := range allFeatures {
		if featureRequiresScopeMetadata(feature) {
			if _, declared := seenScoped[feature]; !declared {
				t.Fatalf("scoped feature %q lacks explicit scope metadata", feature)
			}
		}
	}
}

func featureRequiresScopeMetadata(feature compat.Feature) bool {
	parts := strings.Split(string(feature), ".")
	protocols := []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses, protocolkind.Messages}
	for _, protocol := range protocols {
		if parts[len(parts)-1] == protocol.String() {
			return true
		}
		if len(parts) >= 2 && parts[len(parts)-2] == protocol.String() {
			return true
		}
	}
	_, providerSuffix := profile.ParseProviderID(parts[len(parts)-1])
	return providerSuffix
}

func TestFeatureScopeMetadataDetectionCoversProtocolAndProviderSuffixes(t *testing.T) {
	for _, feature := range []compat.Feature{
		"request.foo.responses",
		"request.foo.messages.anthropic",
		"request.foo.messages.unknown_provider",
		"request.foo.anthropic",
	} {
		if !featureRequiresScopeMetadata(feature) {
			t.Fatalf("feature %q did not require scope metadata", feature)
		}
	}
	if featureRequiresScopeMetadata("request.items.tool_use.name") {
		t.Fatal("ordinary canonical path was mistaken for representation scope")
	}
}

func productionFeatureConstants(t *testing.T) []compat.Feature {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), "feature.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	features := []compat.Feature(nil)
	ast.Inspect(parsed, func(node ast.Node) bool {
		declaration, ok := node.(*ast.GenDecl)
		if !ok || declaration.Tok != token.CONST {
			return true
		}
		for _, spec := range declaration.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			typeName, explicitlyFeature := valueSpec.Type.(*ast.Ident)
			if !explicitlyFeature || typeName.Name != "Feature" {
				continue
			}
			for _, value := range valueSpec.Values {
				literal, ok := value.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("Feature constant must use a string literal: %#v", value)
				}
				raw, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				features = append(features, compat.Feature(raw))
			}
		}
		return false
	})
	if len(features) == 0 {
		t.Fatal("feature.go declares no Feature constants")
	}
	return features
}

func TestFeatureScopeGrammarUsesAuthoritativeProtocolAndProviderValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scoped scopedFeature
		valid  bool
	}{
		{
			name: "declared Responses scope",
			scoped: scopedFeature{
				Feature: compat.RequestPreviousResponseResponses, Base: compat.RequestPreviousResponse, Protocol: protocolkind.Responses,
			},
			valid: true,
		},
		{
			name: "double protocol scope",
			scoped: scopedFeature{
				Feature: "request.previous_response.responses.responses", Base: compat.RequestPreviousResponse, Protocol: protocolkind.Responses,
			},
			valid: false,
		},
		{
			name: "protocol and provider scope",
			scoped: scopedFeature{
				Feature: "tool.web_search.messages.anthropic", Base: "tool.web_search", Protocol: protocolkind.Messages, Provider: profile.ProviderSpecAnthropic,
			},
			valid: true,
		},
		{
			name: "provider without protocol",
			scoped: scopedFeature{
				Feature: "tool.web_search.anthropic", Base: "tool.web_search", Provider: profile.ProviderSpecAnthropic,
			},
		},
		{
			name: "provider before protocol",
			scoped: scopedFeature{
				Feature: "tool.web_search.anthropic.messages", Base: "tool.web_search", Protocol: protocolkind.Messages, Provider: profile.ProviderSpecAnthropic,
			},
		},
		{
			name: "unknown protocol",
			scoped: scopedFeature{
				Feature: "tool.web_search.unknown_protocol", Base: "tool.web_search", Protocol: "unknown_protocol",
			},
		},
		{
			name: "unknown provider",
			scoped: scopedFeature{
				Feature: "tool.web_search.messages.unknown_provider", Base: "tool.web_search", Protocol: protocolkind.Messages, Provider: "unknown_provider",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateScopedFeature(tc.scoped)
			if tc.valid && err != nil {
				t.Fatalf("%q rejected: %v", tc.scoped.Feature, err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("%q accepted", tc.scoped.Feature)
			}
		})
	}
}

func validateFeaturePath(feature compat.Feature) error {
	raw := string(feature)
	if raw == "" || strings.TrimSpace(raw) != raw {
		return fmt.Errorf("empty or padded feature")
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return fmt.Errorf("feature requires a rooted path")
	}
	for _, part := range parts {
		if !featureSegment.MatchString(part) {
			return fmt.Errorf("invalid path segment %q", part)
		}
	}
	approvedRoot := map[string]bool{"request": true, "response": true, "delivery": true, "wire": true, "error": true, "tool": true}
	if !approvedRoot[parts[0]] {
		return fmt.Errorf("unapproved root %q", parts[0])
	}
	return nil
}

func validateScopedFeature(scoped scopedFeature) error {
	if err := validateFeaturePath(scoped.Base); err != nil {
		return fmt.Errorf("invalid base: %w", err)
	}
	if err := validateFeaturePath(scoped.Feature); err != nil {
		return err
	}
	if _, err := protocolkind.ParseProtocolKind(scoped.Protocol.String()); err != nil {
		return err
	}
	if scoped.Provider != "" {
		if _, ok := profile.ParseProviderID(string(scoped.Provider)); !ok {
			return fmt.Errorf("unsupported provider %q", scoped.Provider)
		}
	}

	want := string(scoped.Base) + "." + scoped.Protocol.String()
	if scoped.Provider != "" {
		want += "." + string(scoped.Provider)
	}
	if string(scoped.Feature) != want {
		return fmt.Errorf("got %q, want exact declared scope %q", scoped.Feature, want)
	}
	return nil
}
