package target_config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestFileGrammarDemolitionLedger is the anti-carry guardrail for the
// target_config collapse (epic-08 task 000). It enforces file-grammar-canon.md:
// the package may keep only the allowed authored kinds (component/state/phase/
// effects/*_projection/*.gsx/*_test.go/doc) plus an explicit, shrinking
// allowlist of legacy "bad" files. Each allowlisted file names the epic task
// that deletes it.
//
// The test fails when:
//   - a NEW file matches a forbidden grammar kind (control/flow/render/row/
//     manager/service/workflow/factory/builder/helpers or a vague domain noun)
//     that is not on the allowlist — i.e. garbage was re-introduced; or
//   - an allowlisted file has been removed without being struck from the ledger
//     — so the ledger can only shrink as the epic demolishes files.
//
// Generated *_gsx.go, *_test.go, and .gsx sources are exempt (their grammar is
// enforced separately).
func TestFileGrammarDemolitionLedger(t *testing.T) {
	// The collapse is complete: no legacy file name may be carried forward.
	want := map[string]string{}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	flagged := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gsx.go") {
			continue
		}
		if isForbiddenGrammar(name) {
			flagged[name] = true
		}
	}

	// No new bad file may appear that is not on the allowlist.
	var unexpected []string
	for name := range flagged {
		if _, ok := want[name]; !ok {
			unexpected = append(unexpected, name)
		}
	}
	// An allowlisted file that no longer exists means the ledger is stale: the
	// demolition task succeeded, so strike it here.
	var stale []string
	for name := range want {
		if !flagged[name] {
			if _, err := os.Stat(filepath.Join(".", name)); os.IsNotExist(err) {
				stale = append(stale, name)
			}
		}
	}
	sort.Strings(unexpected)
	sort.Strings(stale)
	if len(unexpected) > 0 {
		t.Fatalf("new forbidden-grammar files appeared in target_config; add them to the demolition ledger with the task that deletes them, or rename by kind (see file-grammar-canon.md): %v", unexpected)
	}
	if len(stale) > 0 {
		t.Fatalf("allowlisted bad files no longer present — strike them from the want map (the ledger may only shrink): %v", stale)
	}
}

// TestTargetConfigUsesOnlyAllowedProductionFileRoles prevents architecture from
// being hidden behind a new filename. The package has one component owner, one
// state owner, one effects owner, pure projections, and GSX component sources.
func TestTargetConfigUsesOnlyAllowedProductionFileRoles(t *testing.T) {
	allowed := map[string]bool{
		"component.go": true, "state.go": true, "effects.go": true, "doc.go": true,
		"catalog_projection.go": true, "provider_projection.go": true,
		"provider_azure_component.go": true,
	}
	for _, path := range mustGlob(t, "*.go") {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gsx.go") || allowed[name] {
			continue
		}
		t.Fatalf("%s is outside target_config's allowed production file grammar", name)
	}
}

func TestTargetConfigGeneratorAdaptersStayNarrow(t *testing.T) {
	want := map[string][]string{
		"provider_azure_component.go": {"target            *TargetConfig", "endpointCommitted bool", "endpointDraft     *tui.State[string]"},
	}
	for path, fields := range want {
		src := mustReadFile(t, path)
		for _, field := range fields {
			if !strings.Contains(src, field) {
				t.Fatalf("%s must retain only its generator-required receiver shape; missing %q", path, field)
			}
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		var actual []string
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.TypeSpec)
			if !ok || decl.Name.Name != "azureProviderForm" {
				return true
			}
			st, ok := decl.Type.(*ast.StructType)
			if !ok {
				t.Fatal("azureProviderForm must be a struct")
			}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					actual = append(actual, name.Name)
				}
			}
			return false
		})
		sort.Strings(actual)
		expected := []string{"endpointCommitted", "endpointDraft", "target"}
		if strings.Join(actual, ",") != strings.Join(expected, ",") {
			t.Fatalf("%s fields = %v, want exactly %v", path, actual, expected)
		}
		for _, forbidden := range []string{"setupState", "catalogResult", "profile.", "providerReadiness", "ReadyAndProbe", "ProbeCatalog"} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s contains forbidden policy dependency %q", path, forbidden)
			}
		}
	}
	for _, removed := range []string{"tail_component.go", "view_component.go", "provider_bedrock_component.go", "provider_custom_component.go"} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("unnecessary generator adapter survives: %s", removed)
		}
	}
}

func TestTargetConfigCoreFileRolesStayNarrow(t *testing.T) {
	for _, path := range []string{"component.go", "state.go", "effects.go"} {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{"\n\tui.New", "\nui.New"} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s must not construct visible UI: %q", path, forbidden)
			}
		}
	}
}

// isForbiddenGrammar reports whether a non-test, non-generated .go filename
// matches a forbidden file kind per file-grammar-canon.md.
func isForbiddenGrammar(name string) bool {
	for _, suf := range []string{
		"_control.go", "_controller.go", "_manager.go", "_service.go",
		"_workflow.go", "_factory.go", "_builder.go", "_flow.go",
	} {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	for _, pre := range []string{"render_", "row_", "control_", "provider_flow_"} {
		if strings.HasPrefix(name, pre) {
			return true
		}
	}
	// Vague domain nouns that must be renamed by kind (projection/effects/state)
	// or merged away.
	switch name {
	case "auth.go", "catalog.go", "create.go", "credential.go", "controls.go",
		"helpers.go", "draft.go", "draft_readmodel.go", "model_catalog.go",
		"picker_options.go", "pickers.go", "protocol_options.go",
		"provider_setup.go", "provider_target_flow.go", "shared_rows.go",
		"bedrock_flow_state.go":
		return true
	}
	return false
}

// TestTargetConfigEscapeUsesSharedBackScopeGrammar forbids bespoke go-tui
// Escape/focus wiring in the root component; root backout goes through
// ui.BackScope only.
func TestTargetConfigEscapeUsesSharedBackScopeGrammar(t *testing.T) {
	src := mustReadFile(t, "component.go")
	if strings.Contains(src, "tui.OnFocused"+"(tui.KeyEscape") {
		t.Fatal("TargetConfig must not bind Escape as a shell-focused handler")
	}
	if !strings.Contains(src, "ui.BackScope(") {
		t.Fatal("TargetConfig must use the shared ui.BackScope grammar")
	}
}

func TestTargetConfigHasNoProviderFlowOrChildBackoutRegistry(t *testing.T) {
	for _, path := range []string{"component.go", "state.go"} {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{
			"ProviderTargetFlow", "activeProviderTargetFlow", "closeAnyOpenDisclosure", "providerFlowBack",
		} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s must not retain %q", path, forbidden)
			}
		}
	}
}

func TestTargetConfigDoesNotOwnPickerCaches(t *testing.T) {
	src := mustReadFile(t, "component.go")
	for _, forbidden := range []string{"providerPickerCache", "fileBrowserCache", "resetPickers"} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("TargetConfig must not retain picker lifecycle %q", forbidden)
		}
	}
}

func TestTargetConfigDoesNotOwnCredentialInteractionState(t *testing.T) {
	for _, path := range []string{"state.go", "component.go"} {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{"CredentialStage", "CredentialEnvName", "CredentialFilePath", "CredentialSecret"} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s must not retain credential interaction state %q", path, forbidden)
			}
		}
	}
}

func TestTargetConfigRootOwnsNoVisibleRowsOrSelectionTransitions(t *testing.T) {
	src := mustReadFile(t, "component.go")
	for _, forbidden := range []string{
		"ui.NewSelectableRow(",
		"ui.NewEditableRow(",
		"ui.NewSelect(",
		"ui.NewSearchPicker(",
		"func (w *TargetConfig) SelectProvider(",
		"func (w *TargetConfig) SelectProtocol(",
		"func (w *TargetConfig) SelectPlacement(",
		"func (w *TargetConfig) SelectModelByID(",
	} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("component.go must retain root lifecycle only; contains %q", forbidden)
		}
	}
}

func TestTargetConfigViewHasExplicitProviderHierarchy(t *testing.T) {
	src := mustReadFile(t, "view.gsx")
	for _, required := range []string{
		"@AzureProviderForm(w)",
		"@CustomProviderForm(w)",
		"@BedrockProviderForm(w)",
		"@ChatGPTProviderForm(w)",
		"@HTTPProviderForm(w)",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("view.gsx must visibly own provider hierarchy; missing %q", required)
		}
	}
	if strings.Contains(src, "@ProviderForm(w)") {
		t.Fatal("view.gsx must not hide provider hierarchy behind ProviderForm")
	}
}

func TestTargetConfigMountsUIRowsWithoutForwardingWrappers(t *testing.T) {
	for _, path := range mustGlob(t, "*.gsx") {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{
			"selectableRowComponent",
			"editableRowComponent",
			"selectComponent",
		} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s must mount ui rows directly; found %q", path, forbidden)
			}
		}
	}
}

func TestCredentialVisibilityHasNoSharedProviderDispatch(t *testing.T) {
	for _, path := range mustGlob(t, "provider*.go") {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{"shouldRenderCredentialRow", "credentialBlockedReason"} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s must not dispatch credential visibility by provider: %q", path, forbidden)
			}
		}
	}
}

func TestViewOwnsTheOnlyTargetTailAndError(t *testing.T) {
	view := mustReadFile(t, "view.gsx")
	for _, required := range []string{"@TargetConfigTail(w)", "@TargetConfigError(w)"} {
		if !strings.Contains(view, required) {
			t.Fatalf("view.gsx must own global form structure: %q", required)
		}
	}
	for _, path := range []string{"provider_http.gsx", "provider_azure.gsx", "provider_bedrock.gsx", "provider_chatgpt.gsx", "provider_custom.gsx"} {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{"TargetConfigTail", "TargetConfigError"} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s must render provider fields only; found %q", path, forbidden)
			}
		}
	}
}

func TestEveryGSXFileDeclaresAComponent(t *testing.T) {
	for _, path := range mustGlob(t, "*.gsx") {
		if !strings.Contains(mustReadFile(t, path), "templ ") {
			t.Fatalf("%s has no declarative component; move non-template code to a semantically named .go file", path)
		}
	}
}

func TestGSXDoesNotDefineControlWrapperTypes(t *testing.T) {
	for _, path := range mustGlob(t, "*.gsx") {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{"providerPickerComponent", "credentialFileBrowser"} {
			if strings.Contains(src, "type "+forbidden) {
				t.Fatalf("%s defines forwarding wrapper %q instead of mounting the ui component directly", path, forbidden)
			}
		}
	}
}

func TestGSXDoesNotOwnTargetConfigTransitionMethods(t *testing.T) {
	for _, path := range mustGlob(t, "*.gsx") {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{
			"func (w *TargetConfig) Select",
			"func (w *TargetConfig) Set",
			"func (w *TargetConfig) Commit",
		} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s owns target-config transition %q; transitions belong in .go", path, forbidden)
			}
		}
	}
}

func TestProjectionsDoNotAcceptTargetConfig(t *testing.T) {
	for _, path := range mustGlob(t, "*_projection.go") {
		if strings.Contains(mustReadFile(t, path), "*TargetConfig") {
			t.Fatalf("%s accepts TargetConfig; projections must accept the values they derive from", path)
		}
	}
}

func TestProjectionsDoNotReadTUIState(t *testing.T) {
	for _, path := range mustGlob(t, "*_projection.go") {
		if strings.Contains(mustReadFile(t, path), ".Get()") {
			t.Fatalf("%s reads reactive tui state; projections must receive concrete values", path)
		}
	}
}

// TestProjectionFilesArePure prevents controllers and component factories from
// being laundered behind the projection suffix. A projection may derive labels,
// options, and readiness from product state; it cannot own UI, lifecycle, or
// effects.
func TestProjectionFilesArePure(t *testing.T) {
	for _, path := range mustGlob(t, "*_projection.go") {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{
			"github.com/grindlemire/go-tui",
			"internal/cockpit/ui",
			"tui.Component",
			"*ui.",
			"BindApp(",
			"UpdateProps(",
			"KeyMap(",
			"Render(",
			"tui.NewState",
			".Set(",
			".Update(",
		} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s is not a pure projection; contains %q", path, forbidden)
			}
		}
	}
}

func TestTargetConfigHasNoPickerLifecyclePhases(t *testing.T) {
	src := mustReadFile(t, "state.go")
	for _, forbidden := range []string{
		"Phase" + "ModelSelection",
		"Phase" + "ProtocolSelection",
		"Phase" + "AuthModeSelection",
		"Phase" + "PlacementSelection",
		"Phase" + "ProviderSetupBlocked",
	} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("feature phase must not encode picker or local UI state: %s", forbidden)
		}
	}
}

func TestTargetConfigOperationStateIsIndependentFromLifecycle(t *testing.T) {
	for _, path := range []string{"state.go", "component.go", "effects.go", "provider_chatgpt.gsx", "tail.gsx"} {
		src := mustReadFile(t, path)
		for _, forbidden := range []string{
			"PhaseAuthPending", "PhaseAuthFailed", "PhaseLoadingCatalog",
			"PhaseCatalogFailed", "PhaseCreateFailed",
		} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s couples operation state to lifecycle through %s", path, forbidden)
			}
		}
	}
}

func TestAzureAuthoringRejectsLegacyResourceRootFallback(t *testing.T) {
	src := mustReadFile(t, "provider_azure_component.go")
	if strings.Contains(src, "NormalizeAzureResourceLocator") {
		t.Fatal("Azure authoring must accept project endpoints only; legacy resource-root compatibility is forbidden")
	}
}

func TestSharedTargetFieldsContainNoProviderWorkflowPolicy(t *testing.T) {
	credential := mustReadFile(t, "credential.gsx")
	for _, forbidden := range []string{"providerSpec string", "providerAllowsNoCredential(r.target)"} {
		if strings.Contains(credential, forbidden) {
			t.Fatalf("credential field must be props-driven; found %q", forbidden)
		}
	}
	tail := mustReadFile(t, "tail.gsx")
	for _, forbidden := range []string{"IsAzureFlow()", "targetUsesInteractiveAuth", `"model", "loading catalog…"`} {
		if strings.Contains(tail, forbidden) {
			t.Fatalf("generic target tail contains provider policy %q", forbidden)
		}
	}
	bedrock := mustReadFile(t, "provider_bedrock.gsx")
	for _, forbidden := range []string{"BedrockMantleRegionFromEndpoint", "target.BaseURL"} {
		if strings.Contains(bedrock, forbidden) {
			t.Fatalf("Bedrock form must author region directly; found %q", forbidden)
		}
	}
}

func TestCredentialFieldImplementationHasNoWorkflowDependencies(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "credential_gsx.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range file.Imports {
		path := strings.Trim(imported.Path.Value, `"`)
		for _, forbidden := range []string{"/profile", "/cockpit/ports", "/cockpit/readmodel"} {
			if strings.HasSuffix(path, forbidden) {
				t.Fatalf("reusable credential field imports workflow dependency %q", path)
			}
		}
	}
	file, err = parser.ParseFile(token.NewFileSet(), "credential_gsx.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == "TargetConfig" {
			t.Fatal("reusable credential field references TargetConfig")
		}
		return true
	})
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	return paths
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
