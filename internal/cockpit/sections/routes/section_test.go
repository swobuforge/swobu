package routes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/features/target_config"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestSection_FocusableRowsFollowExpansion(t *testing.T) {
	model := routeSectionModel()

	collapsed := Section(model, nil)
	collapsedRoot := mountedRoot(t, collapsed)
	collapsedFocusables := countFocusables(collapsedRoot)
	if got, want := collapsedFocusables, 4; got != want {
		t.Fatalf("collapsed focusables = %d, want %d", got, want)
	}

	section := Section(model, nil)
	section.OpenRoute(section.State.Routes[0])
	expanded := mountedRoot(t, section)
	if got := countFocusables(expanded); got <= collapsedFocusables {
		t.Fatalf("expanded focusables = %d, want more than collapsed %d", got, collapsedFocusables)
	}
}

func TestRouteParentRow_ActivationTogglesExpansion(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.toggleRoute(route)
	if got := section.State.ExpandedRoute.Get(); got != route.ID {
		t.Fatalf("expanded route after toggle = %q, want %q", got, route.ID)
	}
	section.toggleRoute(route)
	if got := section.State.ExpandedRoute.Get(); got != "" {
		t.Fatalf("expanded route after second toggle = %q, want empty", got)
	}
}

func TestTargetChildRow_ActivationOpensTargetWithoutCollapsingParent(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.openTarget(target)
	if got := section.State.OpenTarget.Get(); got != target.ID {
		t.Fatalf("opened target = %q, want %q", got, target.ID)
	}
	if got := section.State.ExpandedRoute.Get(); got != route.ID {
		t.Fatalf("expanded route = %q, want still %q", got, route.ID)
	}
}

func TestTargetChildRows_KeepActionColumnForBalancedTargets(t *testing.T) {
	model := readmodel.WorkspaceReadModel{
		ProviderOptions: fixtureProviderOptions(),
		Routes: []readmodel.RouteReadModel{
			{
				ID:        "kimi",
				ModelName: "Kimi-K2.6",
				State:     readmodel.RouteNormal,
				Default:   true,
				Enabled:   true,
				Targets: []readmodel.TargetReadModel{
					{ID: "target-1", Provider: "azure", Model: "Kimi-K2.6", Rank: 1, Weight: 1},
					{ID: "target-2", Provider: "openai", Model: "gpt-4.1-mini", Rank: 1, Weight: 1},
				},
			},
		},
	}
	section := Section(model, nil)
	section.State.ExpandedRoute.Set("kimi")

	rendered := testkit.RenderMountedTrimmed(t, section, 90, 16)
	azureLine := lineContaining(rendered, "azure/Kimi-K2.6")
	openaiLine := lineContaining(rendered, "openai/gpt-4.1-mini")
	if azureLine == "" || openaiLine == "" {
		t.Fatalf("balanced target rows missing:\n%s", rendered)
	}
	azureActionCol := strings.Index(azureLine, "edit ↵")
	openaiActionCol := strings.Index(openaiLine, "edit ↵")
	if azureActionCol < 0 || openaiActionCol < 0 {
		t.Fatalf("target rows missing edit action:\n%s", rendered)
	}
	if azureActionCol != openaiActionCol {
		t.Fatalf("target edit action columns differ: azure=%d openai=%d\n%s\n%s\nframe:\n%s", azureActionCol, openaiActionCol, azureLine, openaiLine, rendered)
	}
}

func TestAddTargetRow_ActivationRecordsLocalIntent(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)
	if got := section.State.AddTargetRoute.Get(); got != route.ID {
		t.Fatalf("add target route = %q, want %q", got, route.ID)
	}
}

func TestAddRouteRowComponent_PreservesRequestedFocusSeed(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.RequestAddRouteFocus()

	row := AddRouteRowComponent(section)
	if row == nil {
		t.Fatal("expected add route row")
	}
	if !row.AutoFocus {
		t.Fatal("requested add route row should autofocus")
	}

	next := AddRouteRowComponent(section)
	if next != row {
		t.Fatal("add route row should be cached while the seed is pending")
	}
	if !next.AutoFocus {
		t.Fatal("add route row autofocus seed should persist until focus lands")
	}
}

func TestAddRouteRowComponent_AppLoopShowsRequestedFocus(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.RequestAddRouteFocus()

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	testkit.AssertFocusedFrame(t, h.Frame(), "> add model route")
}

func TestRouteAdd_DraftReplacesAddRouteLeaf(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 8)
	if strings.Contains(rendered, "add model route") {
		t.Fatalf("open draft should replace add model route leaf:\n%s", rendered)
	}
	assertSubstringsInOrder(t, rendered, "draft", "incomplete", "collapse ↵", "name", "create ↵", "client sends", "model =")
}

func TestRouteAdd_DraftCollapseReturnsToAddRouteLeaf(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()

	DraftParentRowComponent(section).Activate()

	if section.DraftRoute != nil {
		t.Fatal("draft collapse should clear the draft capsule")
	}
	rendered := testkit.RenderMountedTrimmed(t, section, 100, 8)
	if strings.Contains(rendered, "draft") {
		t.Fatalf("collapsed draft should disappear:\n%s", rendered)
	}
	if !strings.Contains(rendered, "add model route") {
		t.Fatalf("collapsed draft should return add model route leaf:\n%s", rendered)
	}
}

func TestRouteAdd_EscapeReturnsToAddRouteLeaf(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()

	if !section.Back() {
		t.Fatal("Back should consume an open draft route")
	}
	if section.DraftRoute != nil {
		t.Fatal("Back should clear the draft capsule")
	}
	rendered := testkit.RenderMountedTrimmed(t, section, 100, 8)
	if !strings.Contains(rendered, "add model route") {
		t.Fatalf("Back should return add model route leaf:\n%s", rendered)
	}
}

func TestRouteAdd_DraftNameMirrorsParentAndClientSends(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	focusRouteFrameUntilContains(t, h, "> name", 20)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'g'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'p'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 't'})

	frame := h.Frame()
	assertSubstringsInOrder(t, frame, "gpt", "incomplete", "client sends", "model = gpt")
	if strings.Contains(frame, "add model route") {
		t.Fatalf("draft typing should keep add model route leaf replaced:\n%s", frame)
	}
}

func TestAddTargetOpen_RendersInlineWorkflowHeader(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	section.TargetConfigs.ProviderOptions = []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		{ProviderSpec: "openrouter", DisplayName: "OpenRouter", SetupHint: "API key"},
		{ProviderSpec: "openai_compatible", DisplayName: "Custom Endpoint", SetupHint: "endpoint"},
	}
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	rendered := testkit.RenderMountedTrimmed(t, section, 220, 20)
	assertSubstringsInOrder(t, rendered, "name", "default", "step 1", "openai/gpt-4.1", "step 2", "anthropic/claude-sonnet", "add target")
	if !strings.Contains(rendered, "add target") || !strings.Contains(rendered, "collapse ↵") {
		t.Fatalf("open add-target config should expose a reversible parent row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "OpenAI") || !strings.Contains(rendered, "OpenRouter") || !strings.Contains(rendered, "Custom Endpoint") {
		t.Fatalf("provider picker should render provider display labels:\n%s", rendered)
	}
	if strings.Contains(rendered, profile.ProviderSetupKeywordSummaryForSpec("openai")) ||
		strings.Contains(rendered, profile.ProviderSetupKeywordSummaryForSpec("openrouter")) ||
		strings.Contains(rendered, profile.ProviderSetupKeywordSummaryForSpec("openai_compatible")) {
		t.Fatalf("provider picker labels should not leak provider setup inventory:\n%s", rendered)
	}
	if strings.Contains(rendered, "provider/model") || strings.Contains(rendered, "model _") || strings.Contains(rendered, "credential _") || strings.Contains(rendered, "base URL _") {
		t.Fatalf("provider picker should not leak provider-setup input rows:\n%s", rendered)
	}
	testkit.AssertVisual("add_target_open").
		Fixture("testdata/routes_section/fixture/add_target_open.txt").
		Viewport(220, 20).
		Now(t, rendered)
}

func TestAddTargetOpen_ParentRowDoesNotJumpHorizontally(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)

	closed := testkit.RenderMountedTrimmed(t, section, 160, 20)
	closedLine := lineContaining(closed, "add target")
	closedCol := strings.Index(closedLine, "add target")
	if closedCol < 0 {
		t.Fatalf("closed add-target row missing:\n%s", closed)
	}

	section.AddTarget(route)
	open := testkit.RenderMountedTrimmed(t, section, 160, 20)
	openLine := lineContaining(open, "add target")
	openCol := strings.Index(openLine, "add target")
	if openCol < 0 {
		t.Fatalf("open add-target row missing:\n%s", open)
	}
	if openCol != closedCol {
		t.Fatalf("add-target row shifted from column %d to %d\nclosed: %q\nopen:   %q", closedCol, openCol, closedLine, openLine)
	}
}

func TestAddTargetOpen_ParentRowCollapseReturnsToAddTargetLeaf(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	config := section.targetAddConfig(route)
	target_config.TargetConfigHeader(config).Activate()

	if got := section.State.AddTargetRoute.Get(); got != "" {
		t.Fatalf("add target route after parent collapse = %q, want empty", got)
	}
	rendered := testkit.RenderMountedTrimmed(t, section, 160, 20)
	if !strings.Contains(rendered, "add target") || !strings.Contains(rendered, "add ↵") {
		t.Fatalf("expected closed add-target leaf after parent collapse:\n%s", rendered)
	}
	if strings.Contains(rendered, "provider") || strings.Contains(rendered, "search") {
		t.Fatalf("provider picker should close after parent collapse:\n%s", rendered)
	}
}

func TestAddTargetOpen_EscapeReturnsToAddTargetRow(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.App().FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if got := section.State.AddTargetRoute.Get(); got != "" {
		t.Fatalf("add target route after escape = %q, want empty", got)
	}
	rendered := h.Frame()
	if !strings.Contains(rendered, "add target") {
		t.Fatalf("expected add target row after escape:\n%s", rendered)
	}
	if strings.Contains(rendered, "search") {
		t.Fatalf("provider picker should close after escape:\n%s", rendered)
	}
}

func TestAddTargetProviderPicker_EscapeReturnsToAddTargetRowFromOption(t *testing.T) {
	model := routeSectionModel()
	model.Routes = []readmodel.RouteReadModel{{
		ID:        "z-glm",
		ModelName: "z/glm",
		State:     readmodel.RouteNormal,
		Enabled:   true,
	}}
	section := Section(model, fakeRouteCommands{})
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	if frame := h.Frame(); !strings.Contains(frame, "> ChatGPT") {
		t.Fatalf("test did not reach provider picker option focus:\n%s", frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if got := section.State.AddTargetRoute.Get(); got != "" {
		t.Fatalf("add target route after picker option escape = %q, want empty", got)
	}
	rendered := h.Frame()
	if !strings.Contains(rendered, "add target") || !strings.Contains(rendered, "add ↵") {
		t.Fatalf("expected closed add target row after picker option escape:\n%s", rendered)
	}
	if strings.Contains(rendered, "search") || strings.Contains(rendered, "select ↵") {
		t.Fatalf("provider picker should close after picker option escape:\n%s", rendered)
	}
}

func lineContaining(frame, needle string) string {
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func assertRouteRenderedLineContains(t *testing.T, frame string, parts ...string) {
	t.Helper()
	for _, line := range strings.Split(frame, "\n") {
		offset := 0
		matched := true
		for _, part := range parts {
			idx := strings.Index(line[offset:], part)
			if idx < 0 {
				matched = false
				break
			}
			offset += idx + len(part)
		}
		if matched {
			return
		}
	}
	t.Fatalf("rendered frame missing line parts %q:\n%s", strings.Join(parts, " "), frame)
}

// ---------------------------------------------------------------------------
// Provider picker search wireframe fixtures (265)
// ---------------------------------------------------------------------------

func TestAddTargetOpen_ProviderPickerSearch(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	section.TargetConfigs.ProviderOptions = []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		{ProviderSpec: "chatgpt", DisplayName: "ChatGPT", SetupHint: "browser login"},
		{ProviderSpec: "anthropic", DisplayName: "Anthropic", SetupHint: "API key"},
		{ProviderSpec: "openrouter", DisplayName: "OpenRouter", SetupHint: "API key"},
		{ProviderSpec: "ollama", DisplayName: "Ollama", SetupHint: "none"},
		{ProviderSpec: "azure", DisplayName: "Azure AI Foundry", SetupHint: "endpoint"},
		{ProviderSpec: "openai_compatible", DisplayName: "Custom Endpoint", SetupHint: "endpoint"},
	}
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	// Type "open" into the focused provider picker (it auto-focuses on open).
	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	for _, r := range "open" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}

	rendered := h.Frame()
	assertSubstringsInOrder(t, rendered, "add target", "provider", "search", "open", "OpenAI", "OpenRouter", "Custom Endpoint")
	if !strings.Contains(rendered, "3 of 7 shown") {
		t.Fatalf("expected '3 of 7 shown' footer after filtering:\n%s", rendered)
	}
	if strings.Contains(rendered, "Ollama") {
		t.Fatalf("filtered list should NOT contain Ollama:\n%s", rendered)
	}
	if strings.Contains(rendered, "Anthropic") {
		t.Fatalf("filtered list should NOT contain Anthropic:\n%s", rendered)
	}
}

func TestAddTargetOpen_ProviderPickerSelectsBedrockMantleFlow(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	for _, r := range "bed" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	rendered := h.Frame()
	assertRouteRenderedLineContains(t, rendered, "provider", "AWS Bedrock", "change", "↵")
	assertRouteRenderedLineContains(t, rendered, "connection", "Mantle", "default")
	assertRouteRenderedLineContains(t, rendered, "region", "required", "choose", "↵")
	assertRouteRenderedLineContains(t, rendered, "model", "blocked", "region first")
	if strings.Contains(rendered, "new target · AWS Bedrock") &&
		!strings.Contains(rendered, "connection") {
		t.Fatalf("Bedrock selection fell through to the generic provider target flow:\n%s", rendered)
	}
}

func TestAddTargetOpen_ProviderPickerCanonicalizesBedrockSpecBeforeFlow(t *testing.T) {
	model := routeSectionModel()
	model.ProviderOptions = []readmodel.ProviderOptionReadModel{
		{ProviderSpec: " bedrock ", DisplayName: "AWS Bedrock", SetupHint: "region / auth"},
	}
	section := Section(model, fakeRouteCommands{})
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	rendered := h.Frame()
	assertRouteRenderedLineContains(t, rendered, "provider", "AWS Bedrock", "change", "↵")
	assertRouteRenderedLineContains(t, rendered, "connection", "Mantle", "default")
	assertRouteRenderedLineContains(t, rendered, "region", "required", "choose", "↵")
	if strings.Contains(rendered, "region            enter region") || strings.Contains(rendered, "region enter region edit ↵") {
		t.Fatalf("Bedrock provider picker selection fell through to generic endpoint input:\n%s", rendered)
	}
}

func TestAddTargetOpen_ProviderPickerNoResults(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	section.TargetConfigs.ProviderOptions = []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		{ProviderSpec: "ollama", DisplayName: "Ollama", SetupHint: "none"},
	}
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)
	section.AddTarget(route)

	// Type "xyz" into the focused provider picker (no matches).
	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	for _, r := range "xyz" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}

	rendered := h.Frame()
	if !strings.Contains(rendered, "0 of 2 shown") {
		t.Fatalf("expected '0 of 2 shown' footer for empty search:\n%s", rendered)
	}
	if !strings.Contains(rendered, "provider") {
		t.Fatalf("expected 'provider' title in empty picker:\n%s", rendered)
	}
}

// ---------------------------------------------------------------------------
// Target edit config tests — existing edits reuse the add-target mutation seam.
// ---------------------------------------------------------------------------

func TestRouteSection_TargetEditOpensMutationWorkflow(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.toggleRoute(route)
	section.openTarget(target)

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 20)
	assertSubstringsInOrder(t, rendered, "edit target · OpenAI", "provider", "OpenAI", "fixed", "model", "routing", "step 1", "delete", "target")
	if strings.Contains(rendered, "openai/gpt-4.1                                                        edit ↵") {
		t.Fatalf("open target editor must replace the closed target row, not render both:\n%s", rendered)
	}
	if strings.Contains(rendered, "save ↵") {
		t.Fatalf("target edit should not render a whole-target save:\n%s", rendered)
	}
	if strings.Contains(rendered, "provider/model") {
		t.Fatalf("target edit should not render the old raw provider/model row:\n%s", rendered)
	}
}

func TestTargetEditWorkflow_StateSurvivesRender(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.toggleRoute(route)
	section.openTarget(target)

	wf := section.targetEditConfig(route, target)
	if wf == nil {
		t.Fatal("expected target edit workflow after openTarget")
	}
	wf.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.ProviderSpec = "typed-provider"
		return d
	})
	_ = testkit.RenderMountedString(t, section, 100, 20)

	if got := section.targetEditConfig(route, target).Draft.Get().ProviderSpec; got != "typed-provider" {
		t.Fatalf("workflow provider after render = %q, want typed-provider", got)
	}
}

func TestRouteSection_AzureTargetEditRefreshKeepsEditSeed(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{})
	route := section.State.Routes[0]
	target := readmodel.TargetReadModel{
		ID:               "azure-1",
		Provider:         "azure",
		Model:            "gpt-5.4",
		ProviderProtocol: "responses",
		BaseURL:          "https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837",
		CredentialRef:    "secret:azure",
		Rank:             1,
	}
	section.State.Routes[0].Targets = []readmodel.TargetReadModel{target}
	section.toggleRoute(route)
	section.openTarget(target)

	rendered := testkit.RenderMountedTrimmed(t, section, 120, 24)
	_ = testkit.RenderMountedTrimmed(t, section, 120, 24)

	if strings.Contains(rendered, "search            _") {
		t.Fatalf("azure target edit should not fall back to provider picker:\n%s", rendered)
	}
	if strings.Contains(rendered, "azure/gpt-5.4                                                          edit ↵") {
		t.Fatalf("azure target edit must replace the closed target row, not render both:\n%s", rendered)
	}
	assertSubstringsInOrder(t, rendered, "edit target · Azure AI", "provider", "Azure AI", "fixed", "credential", "secret", "change", "deployment", "gpt-5.4")
}

func TestRouteAdd_DraftRowAppearsInSection(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 8)
	testkit.AssertVisual("draft_row").
		Fixture("testdata/routes_section/fixture/draft_row.txt").
		Viewport(100, 8).
		Now(t, rendered)
}

func TestRouteAdd_CreateOpensTargetAddForThisRoute(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.addRoute()
	section.DraftRoute.Open()

	section.createDraftRoute("custom-route")

	if got := section.State.ExpandedRoute.Get(); got != "custom-route" {
		t.Fatalf("expanded route = %q, want custom-route", got)
	}
	// Add target form stays closed — operator must explicitly choose to add.
	if got := section.State.AddTargetRoute.Get(); got != "" {
		t.Fatalf("add target route = %q, want empty", got)
	}
	route := section.State.Routes[len(section.State.Routes)-1]
	if route.State != readmodel.RouteNormal || len(route.Targets) != 0 {
		t.Fatalf("draft route = %#v, want normal route with no targets", route)
	}
	if got, want := route.RowValue(), "incomplete · no targets"; got != want {
		t.Fatalf("draft route row value = %q, want %q", got, want)
	}
}

func TestRouteAdd_FirstTargetSaveUsesDraftRoute(t *testing.T) {
	var request ports.SaveTargetRequest
	section := Section(routeSectionModel(), fakeRouteCommands{
		saveTarget: func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
			request = req
			return readmodel.TargetReadModel{ID: "target-new", Provider: req.Draft.ProviderSpec, Model: req.Draft.ModelID, Rank: req.Draft.Rank, Weight: req.Draft.Weight}, nil
		},
	})
	section.addRoute()
	section.createDraftRoute("custom-route")
	route := section.State.Routes[len(section.State.Routes)-1]

	// Operator must explicitly open the add-target form for the newly created route.
	section.AddTarget(route)

	wf := section.targetAddConfig(route)
	wf.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.ProviderSpec = "openai_compatible"
		return d
	})
	wf.SelectedModel.Set(readmodel.ModelDeploymentReadModel{
		ID:        "custom-route",
		Name:      "custom-route",
		ModelName: "custom-route",
	})
	wf.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.ProviderProtocol = "chat_completions"
		return d
	})
	wf.Create(context.Background())

	if request.RouteID != "custom-route" || request.Draft.ModelID != "custom-route" {
		t.Fatalf("save target request = %+v, want draft route/model custom-route", request)
	}
	savedRoute := section.State.Routes[len(section.State.Routes)-1]
	if savedRoute.State != readmodel.RouteNormal {
		t.Fatalf("saved route state = %#v, want normal after first target save", savedRoute.State)
	}
	if got, want := savedRoute.RowValue(), "1 target"; got != want {
		t.Fatalf("saved route row value = %q, want %q", got, want)
	}

	rendered := testkit.RenderMountedTrimmed(t, section, 220, 20)
	if !strings.Contains(rendered, "custom-route") {
		t.Fatalf("expected new route in rendered frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "1 target") {
		t.Fatalf("expected created route target count in rendered frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "openai_compatible/custom-route") {
		t.Fatalf("expected created target row in rendered frame:\n%s", rendered)
	}
	testkit.AssertVisual("add_target_created").
		Fixture("testdata/routes_section/fixture/add_target_created.txt").
		Viewport(220, 20).
		Now(t, rendered)
}

func TestRouteSection_UpdatePropsRefreshesOpenTargetAddConfig(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.AddTarget(route)

	wf := section.targetAddConfig(route)
	if wf == nil {
		t.Fatal("expected target add workflow")
	}

	fresh := Section(routeSectionModel(), nil)
	fresh.State.Routes[0].Targets = append(fresh.State.Routes[0].Targets, readmodel.TargetReadModel{
		ID:       "fresh-target",
		Provider: "openai",
		Model:    "gpt-4.1",
		Rank:     2,
		Weight:   1,
	})

	section.UpdateProps(fresh)

	if got, want := len(wf.Route.Targets), len(fresh.State.Routes[0].Targets); got != want {
		t.Fatalf("target add workflow route targets = %d, want %d", got, want)
	}
}

func TestRouteEdit_RenameMovesOpenTargetAddConfig(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.AddTarget(route)

	wf := section.targetAddConfig(route)
	if wf == nil {
		t.Fatal("expected target add workflow")
	}

	renamed := route
	renamed.ID = "gpt-renamed"
	renamed.ModelName = "gpt-renamed"
	section.saveRoute(route.ID, renamed)

	if got := section.State.AddTargetRoute.Get(); got != "gpt-renamed" {
		t.Fatalf("add target route = %q, want gpt-renamed", got)
	}
	if section.TargetConfigs.HasAdd(route.ID) {
		t.Fatal("old target add workflow key should be cleared after rename")
	}
	moved := section.TargetConfigs.CachedAdd("gpt-renamed")
	if moved == nil {
		t.Fatal("renamed route should keep target add workflow")
	}
	if moved != wf {
		t.Fatal("rename should reuse the open target add workflow instance")
	}
	if got := moved.Route.ID; got != "gpt-renamed" {
		t.Fatalf("workflow route id = %q, want gpt-renamed", got)
	}
}

func TestTargetEditSaveUsesOriginalTargetWhenExpansionChanges(t *testing.T) {
	var request ports.SaveTargetRequest
	section := Section(routeSectionModel(), fakeRouteCommands{
		saveTarget: func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
			request = req
			return readmodel.TargetReadModel{
				ID:       req.TargetID,
				Provider: req.Draft.ProviderSpec,
				Model:    req.Draft.ModelID,
				Rank:     req.Draft.Rank,
				Weight:   req.Draft.Weight,
			}, nil
		},
	})
	originalRoute := section.State.Routes[0]
	otherRoute := section.State.Routes[1]
	target := originalRoute.Targets[0]
	section.openTarget(target)

	wf := section.targetEditConfig(originalRoute, target)
	if wf == nil {
		t.Fatal("expected target edit workflow")
	}

	// Operator switches to another route while editing — the workflow was
	// created for originalRoute:target-1, so the save should still target
	// originalRoute and update target-1.
	section.OpenRoute(otherRoute)
	wf.SelectProvider("openai_compatible")
	wf.SelectedModel.Set(readmodel.ModelDeploymentReadModel{
		ID:                      "gpt-4",
		Name:                    "gpt-4",
		ModelName:               "gpt-4",
		DefaultProviderProtocol: "chat_completions",
	})
	wf.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.ProviderProtocol = "chat_completions"
		return d
	})
	wf.Create(context.Background())

	if request.RouteID != originalRoute.ID || request.TargetID != target.ID {
		t.Fatalf("save request route/target = %q/%q, want %q/%q", request.RouteID, request.TargetID, originalRoute.ID, target.ID)
	}
	wantWeight := target.Weight
	if wantWeight <= 0 {
		wantWeight = 1
	}
	if request.Draft.Rank != target.Rank || request.Draft.Weight != wantWeight {
		t.Fatalf("save request rank/weight = %d/%d, want existing %d/%d", request.Draft.Rank, request.Draft.Weight, target.Rank, wantWeight)
	}
	if got := section.State.Routes[0].Targets[0].Provider; got != "openai_compatible" {
		t.Fatalf("original route target provider = %q, want openai_compatible", got)
	}
	if got, want := len(section.State.Routes[1].Targets), 1; got != want {
		t.Fatalf("other route target count = %d, want unchanged %d", got, want)
	}
}

func TestTargetDelete_ConfirmRemovesTargetAndSyncsState(t *testing.T) {
	var deletedTargetID readmodel.TargetID
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(_ context.Context, req ports.DeleteTargetRequest) error {
			deletedTargetID = req.TargetID
			return nil
		},
	})
	originalRoute := section.State.Routes[0]
	target := originalRoute.Targets[0]
	section.openTarget(target)

	if err := section.deleteTargetAndClose(originalRoute.ID, target.ID); err != nil {
		t.Fatalf("deleteTargetAndClose: %v", err)
	}

	if deletedTargetID != target.ID {
		t.Fatalf("deleted target id = %q, want %q", deletedTargetID, target.ID)
	}
	if got, want := len(section.State.Routes[0].Targets), 1; got != want {
		t.Fatalf("targets after delete = %d, want %d", got, want)
	}
	if got := section.State.OpenTarget.Get(); got != "" {
		t.Fatalf("open target = %q, want closed", got)
	}
}

func TestTargetDelete_ConfirmRenumbersSteps(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	// Simulate step 1 with 1 target, step 2 with 2 targets
	section.State.Routes[0].Targets = []readmodel.TargetReadModel{
		{ID: "target-a", Provider: "openai", Model: "gpt-4", Rank: 1},
		{ID: "target-b", Provider: "openai", Model: "gpt-3.5", Rank: 2},
		{ID: "target-c", Provider: "anth", Model: "claude", Rank: 2},
	}
	targetB := section.State.Routes[0].Targets[1]
	if err := section.deleteTargetAndClose(route.ID, targetB.ID); err != nil {
		t.Fatalf("deleteTargetAndClose: %v", err)
	}

	if got, want := len(section.State.Routes[0].Targets), 2; got != want {
		t.Fatalf("targets after delete = %d, want %d", got, want)
	}
	// After deleting one of two targets in step 2, step 2 should now have
	// rank 2 (unchanged, still contiguous).
	if got := section.State.Routes[0].Targets[1].Rank; got != 2 {
		t.Fatalf("remaining step-2 target rank = %d, want 2", got)
	}
}

func TestTargetDelete_ConfirmOnLastTargetRemovesStep(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	// Simulate step 1 with 1 target, step 2 with 1 target
	section.State.Routes[0].Targets = []readmodel.TargetReadModel{
		{ID: "target-a", Provider: "openai", Model: "gpt-4", Rank: 1},
		{ID: "target-b", Provider: "anth", Model: "claude", Rank: 2},
	}
	targetB := section.State.Routes[0].Targets[1]
	if err := section.deleteTargetAndClose(route.ID, targetB.ID); err != nil {
		t.Fatalf("deleteTargetAndClose: %v", err)
	}

	if got, want := len(section.State.Routes[0].Targets), 1; got != want {
		t.Fatalf("targets after delete = %d, want %d", got, want)
	}
	// After deleting step 2's only target, step numbering should collapse:
	// the remaining target should stay rank 1.
	if got := section.State.Routes[0].Targets[0].Rank; got != 1 {
		t.Fatalf("remaining target rank = %d, want 1", got)
	}
}

func TestTargetDelete_ConfirmOnOnlyTargetRemovesRoute(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	// Simulate step 1 with 1 target
	section.State.Routes[0].Targets = []readmodel.TargetReadModel{
		{ID: "target-a", Provider: "openai", Model: "gpt-4", Rank: 1},
	}
	if err := section.deleteTargetAndClose(route.ID, readmodel.TargetID("target-a")); err != nil {
		t.Fatalf("deleteTargetAndClose: %v", err)
	}

	if got, want := len(section.State.Routes), 1; got != want {
		t.Fatalf("routes after last target delete = %d, want %d", got, want)
	}
	if section.State.Routes[0].ID == route.ID {
		t.Fatal("empty route should be removed after deleting its last target")
	}
	if got := section.State.ExpandedRoute.Get(); got != "" {
		t.Fatalf("expanded route = %q, want closed", got)
	}
	if !section.State.Routes[0].Default {
		t.Fatal("remaining target-backed route should become default after deleting default route")
	}
}

func TestTargetDelete_FailureDoesNotMutateLocalRouteState(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return fmt.Errorf("target delete failed") },
	})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.OpenTargetEditor(route, target)

	err := section.deleteTargetAndClose(route.ID, target.ID)
	if err == nil || err.Error() != "target delete failed" {
		t.Fatalf("deleteTargetAndClose error = %v, want target delete failed", err)
	}
	if got, want := len(section.State.Routes[0].Targets), len(route.Targets); got != want {
		t.Fatalf("targets after failed delete = %d, want %d", got, want)
	}
	if got := section.State.OpenTarget.Get(); got != target.ID {
		t.Fatalf("open target after failed delete = %q, want %q", got, target.ID)
	}
}

func TestTargetDeleteUsesWorkflowRouteWhenExpansionChanges(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	originalRoute := section.State.Routes[0]
	otherRoute := section.State.Routes[1]
	target := originalRoute.Targets[0]
	section.openTarget(target)
	wf := section.targetEditConfig(originalRoute, target)
	if wf == nil {
		t.Fatal("expected target edit workflow")
	}

	section.OpenRoute(otherRoute)
	// Delete confirmation is section-owned; changing route expansion must not
	// retarget the already-open edit workflow.
	if got, want := len(section.State.Routes[0].Targets), 2; got != want {
		t.Fatalf("original route target count = %d, want unchanged %d", got, want)
	}
	if got, want := len(section.State.Routes[1].Targets), 1; got != want {
		t.Fatalf("other route target count = %d, want unchanged %d", got, want)
	}
}

func TestRouteEdit_RenameExistingRoute(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		saveRoute: func(_ context.Context, req ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
			route := routeSectionModel().Routes[0]
			route.ID = readmodel.RouteID(req.ModelName)
			route.ModelName = req.ModelName
			return route, nil
		},
	})
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)

	section.submitRouteName(route.ID, "gpt-renamed")

	if section.State.Routes[0].ID != "gpt-renamed" || section.State.Routes[0].ModelName != "gpt-renamed" {
		t.Fatalf("renamed route = %#v, want gpt-renamed", section.State.Routes[0])
	}
	if section.State.ExpandedRoute.Get() != "gpt-renamed" {
		t.Fatalf("expanded route = %q, want gpt-renamed", section.State.ExpandedRoute.Get())
	}
}

func TestRouteEdit_SetDefaultUpdatesOnlyOneRoute(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		saveRoute: func(_ context.Context, req ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
			route := routeSectionModel().Routes[1]
			route.Default = req.Default
			return route, nil
		},
	})
	route := section.State.Routes[1]

	section.setRouteDefault(route.ID)

	if section.State.Routes[0].Default {
		t.Fatal("previous default route should no longer be default")
	}
	if !section.State.Routes[1].Default {
		t.Fatal("selected route should be default")
	}
}

func TestRouteDefaultRow_NoTargetsDoesNotAdvertiseDeadAction(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[1]
	route.Targets = nil
	section.State.Routes[1] = route
	section.State.ExpandedRoute.Set(route.ID)

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 24)
	assertSubstringsInOrder(t, rendered, "default", "no", "target first")
	if strings.Contains(rendered, "make default") {
		t.Fatalf("zero-target route must not advertise make default action:\n%s", rendered)
	}
}

func TestRouteDelete_RemovesRouteAndRefreshes(t *testing.T) {
	var request ports.DeleteRouteRequest
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteRoute: func(_ context.Context, req ports.DeleteRouteRequest) error {
			request = req
			return nil
		},
	})
	route := section.State.Routes[1]
	section.State.ExpandedRoute.Set(route.ID)

	if err := section.confirmDeleteRoute(route.ID); err != nil {
		t.Fatalf("confirmDeleteRoute: %v", err)
	}

	if request.RouteID != route.ID {
		t.Fatalf("delete route request = %+v, want route %q", request, route.ID)
	}
	if got := len(section.State.Routes); got != 1 {
		t.Fatalf("routes after delete = %d, want 1", got)
	}
	if section.State.ExpandedRoute.Get() != "" {
		t.Fatalf("expanded route = %q, want empty", section.State.ExpandedRoute.Get())
	}
	if got := section.State.FocusRoute.Get(); got != "gpt" {
		t.Fatalf("focus route after deleting last route = %q, want previous route %q", got, "gpt")
	}
}

func TestRouteDelete_FocusNextRouteWhenNextExists(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteRoute: func(context.Context, ports.DeleteRouteRequest) error { return nil },
	})
	first := section.State.Routes[0]
	section.State.ExpandedRoute.Set(first.ID)

	if err := section.confirmDeleteRoute(first.ID); err != nil {
		t.Fatalf("confirmDeleteRoute: %v", err)
	}
	if got := section.State.FocusRoute.Get(); got != "local" {
		t.Fatalf("focus route after deleting first route = %q, want next route %q", got, "local")
	}
}

func TestRouteDelete_FocusAddRouteWhenEmpty(t *testing.T) {
	model := routeSectionModel()
	model.Routes = model.Routes[:1]
	section := Section(model, fakeRouteCommands{
		deleteRoute: func(context.Context, ports.DeleteRouteRequest) error { return nil },
	})
	only := section.State.Routes[0]
	section.State.ExpandedRoute.Set(only.ID)

	if err := section.confirmDeleteRoute(only.ID); err != nil {
		t.Fatalf("confirmDeleteRoute: %v", err)
	}
	if got := section.State.FocusRoute.Get(); got != "" {
		t.Fatalf("focus route after deleting only route = %q, want empty", got)
	}
	if !section.addRouteFocusPending {
		t.Fatal("deleting the only route should request add-route focus")
	}
}

func TestRouteDelete_RowSitsBelowAddTargetAndHidesDuringAddTarget(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)

	rendered := testkit.RenderMountedTrimmed(t, section, 220, 24)
	assertSubstringsInOrder(t, rendered, "add target", "delete", "model route", "delete ↵")

	section.AddTarget(route)
	adding := testkit.RenderMountedTrimmed(t, section, 220, 24)
	if strings.Contains(adding, "delete            model route") {
		t.Fatalf("route delete row must hide while add-target workflow is active:\n%s", adding)
	}
}

func TestRouteSection_TargetEditWorkflowStaysOpenUntilExplicitClose(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.toggleRoute(route)
	section.openTarget(target)

	wf := section.targetEditConfig(route, target)
	if wf == nil {
		t.Fatal("expected target edit workflow")
	}
	if got := len(section.State.Routes[0].Targets); got != 2 {
		t.Fatalf("targets after no-op interaction = %d, want 2", got)
	}
	if got := section.State.OpenTarget.Get(); got != target.ID {
		t.Fatalf("open target = %q, want still %q", got, target.ID)
	}
}

func TestRouteDelete_ClearsOpenTargetAddConfig(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	section.AddTarget(route)

	section.deleteRoute(route.ID)

	if got := section.State.AddTargetRoute.Get(); got != "" {
		t.Fatalf("add target route = %q, want empty", got)
	}
	if section.TargetConfigs.HasAdd(route.ID) {
		t.Fatal("deleted route should remove cached target add workflow")
	}
}

func TestRouteDelete_FailureStaysLocalToDeleteRow(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteRoute: func(context.Context, ports.DeleteRouteRequest) error { return fmt.Errorf("route delete blew up") },
	})
	route := section.State.Routes[0]
	section.State.ExpandedRoute.Set(route.ID)

	rendered := testkit.RenderMountedTrimmed(t, section, 220, 24)
	assertSubstringsInOrder(t, rendered, "add target", "delete", "model route", "delete ↵")

	h, err := testkit.NewHarness(section)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	focusRouteFrameUntilContains(t, h, "> delete            model route", 20)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame := h.Frame()
	if !strings.Contains(frame, "delete gpt?") || !strings.Contains(frame, "confirm ↵") {
		t.Fatalf("expected confirm state:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame = h.Frame()
	if !strings.Contains(frame, "delete failed") || !strings.Contains(frame, "retry ↵") || !strings.Contains(frame, "route delete blew up") {
		t.Fatalf("expected local failure state:\n%s", frame)
	}
}

func TestRouteDelete_ClearsOpenTargetEditWorkflow(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.OpenTargetEditor(route, target)

	section.deleteRoute(route.ID)

	if got := section.State.OpenTarget.Get(); got != "" {
		t.Fatalf("open target = %q, want empty", got)
	}
	if section.TargetConfigs.HasEdit(route, target.ID) {
		t.Fatal("deleted route should remove cached target edit workflow")
	}
}

func TestTargetEdit_DeleteConfirmationStaysInTargetConfig(t *testing.T) {
	var deleted ports.DeleteTargetRequest
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(_ context.Context, request ports.DeleteTargetRequest) error {
			deleted = request
			return nil
		},
	})
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.OpenTargetEditor(route, target)
	config := section.targetEditConfig(route, target)

	if config.OnDeleteConfirmed == nil {
		t.Fatal("edit target config missing delete callback")
	}
	config.DeleteArmed.Set(true)

	if got := section.State.OpenTarget.Get(); got != target.ID {
		t.Fatalf("open target = %q, want target editor still open", got)
	}
	rendered := testkit.RenderMountedTrimmed(t, section, 100, 18)
	assertSubstringsInOrder(t, rendered, "edit target · OpenAI", "delete", "delete target?", "confirm ↵")
	if strings.Contains(rendered, "openai/gpt-4.1                                                        edit ↵") {
		t.Fatalf("target delete confirmation must stay inside the open target editor, not alongside a duplicate closed row:\n%s", rendered)
	}
	if strings.Contains(rendered, "delete openai/gpt-4.1") {
		t.Fatalf("target confirmation should not render as route-section long label:\n%s", rendered)
	}

	if err := config.OnDeleteConfirmed(); err != nil {
		t.Fatalf("OnDeleteConfirmed: %v", err)
	}

	if deleted.TargetID != target.ID {
		t.Fatalf("deleted target = %q, want %q", deleted.TargetID, target.ID)
	}
	if got := len(section.State.Routes[0].Targets); got != 1 {
		t.Fatalf("targets after delete = %d, want 1", got)
	}
}

type fakeRouteCommands struct {
	saveRoute    func(context.Context, ports.SaveRouteRequest) (readmodel.RouteReadModel, error)
	saveTarget   func(context.Context, ports.SaveTargetRequest) (readmodel.TargetReadModel, error)
	deleteRoute  func(context.Context, ports.DeleteRouteRequest) error
	deleteTarget func(context.Context, ports.DeleteTargetRequest) error
}

func (f fakeRouteCommands) SaveRoute(ctx context.Context, request ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
	if f.saveRoute != nil {
		return f.saveRoute(ctx, request)
	}
	return readmodel.RouteReadModel{
		ID:        readmodel.RouteID(request.ModelName),
		ModelName: request.ModelName,
		Enabled:   request.Enabled,
		Default:   request.Default,
	}, nil
}

func (f fakeRouteCommands) DeleteRoute(ctx context.Context, request ports.DeleteRouteRequest) error {
	if f.deleteRoute != nil {
		return f.deleteRoute(ctx, request)
	}
	return nil
}

func (f fakeRouteCommands) SaveTarget(ctx context.Context, request ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
	if f.saveTarget != nil {
		return f.saveTarget(ctx, request)
	}
	return readmodel.TargetReadModel{
		ID:       request.TargetID,
		Provider: request.Draft.ProviderSpec,
		Model:    request.Draft.ModelID,
		Rank:     request.Draft.Rank,
		Weight:   request.Draft.Weight,
	}, nil
}

func (f fakeRouteCommands) DeleteTarget(ctx context.Context, request ports.DeleteTargetRequest) error {
	if f.deleteTarget != nil {
		return f.deleteTarget(ctx, request)
	}
	return errors.New("delete target not wired")
}

func countFocusables(root *tui.Element) int {
	return len(collectFocusables(root))
}

func focusRouteFrameUntilContains(t *testing.T, h *testkit.MockAppHarness, needle string, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		if strings.Contains(h.Frame(), needle) {
			return
		}
		h.FocusNext()
	}
	t.Fatalf("frame never contained %q after %d focus steps:\n%s", needle, limit, h.Frame())
}

func collectFocusables(root *tui.Element) []tui.Focusable {
	var focusables []tui.Focusable
	root.WalkFocusables(func(f tui.Focusable) {
		focusables = append(focusables, f)
	})
	return focusables
}

func activate(t *testing.T, focusable tui.Focusable) {
	t.Helper()
	el, ok := focusable.(*tui.Element)
	if !ok {
		t.Fatalf("focusable is %T, want *tui.Element", focusable)
	}
	if !el.Activate() {
		t.Fatal("focusable did not handle activation")
	}
}

func mountedRoot(t *testing.T, component tui.Component) *tui.Element {
	t.Helper()
	h, err := testkit.NewHarness(component)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	h.Open()
	t.Cleanup(h.Close)
	return h.App().Root()
}

func routeSectionModel() readmodel.WorkspaceReadModel {
	return readmodel.WorkspaceReadModel{
		ProviderOptions: fixtureProviderOptions(),
		Routes: []readmodel.RouteReadModel{
			{
				ID:        "gpt",
				ModelName: "gpt",
				State:     readmodel.RouteNormal,
				Default:   true,
				Enabled:   true,
				Targets: []readmodel.TargetReadModel{
					{ID: "target-1", Provider: "openai", Model: "gpt-4.1", Rank: 1},
					{ID: "target-2", Provider: "anthropic", Model: "claude-sonnet", Rank: 2},
				},
			},
			{
				ID:        "local",
				ModelName: "local",
				State:     readmodel.RouteNormal,
				Enabled:   true,
				Targets: []readmodel.TargetReadModel{
					{ID: "local-1", Provider: "ollama", Model: "llama3.2", Rank: 1},
				},
			},
		},
	}
}

func fixtureProviderOptions() []readmodel.ProviderOptionReadModel {
	return []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		{ProviderSpec: "chatgpt", DisplayName: "ChatGPT", SetupHint: "browser login"},
		{ProviderSpec: "anthropic", DisplayName: "Anthropic", SetupHint: "API key"},
		{ProviderSpec: "openrouter", DisplayName: "OpenRouter", SetupHint: "API key"},
		{ProviderSpec: "ollama", DisplayName: "Ollama", SetupHint: "none"},
		{ProviderSpec: "azure", DisplayName: "Azure AI", SetupHint: "endpoint"},
		{ProviderSpec: "openai_compatible", DisplayName: "Custom Endpoint", SetupHint: "endpoint"},
		{ProviderSpec: "bedrock", DisplayName: "AWS Bedrock", SetupHint: "endpoint"},
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSub(s, substr))
}

func containsSub(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func assertSubstringsInOrder(t *testing.T, s string, substrings ...string) {
	t.Helper()
	pos := 0
	for _, substr := range substrings {
		idx := strings.Index(s[pos:], substr)
		if idx < 0 {
			t.Fatalf("rendered output missing %q:\n%s", substr, s)
		}
		pos += idx + len(substr)
	}
}
