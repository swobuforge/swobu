package routing

import (
	"context"
	"strconv"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type routingTestScope struct {
	values map[string]any
	prefix string
}

func (s routingTestScope) Get(slot int) (any, bool) {
	key := s.prefix + strconv.Itoa(slot)
	v, ok := s.values[key]
	return v, ok
}

func (s routingTestScope) Set(slot int, value any) {
	key := s.prefix + strconv.Itoa(slot)
	s.values[key] = value
}

func (s routingTestScope) WithPrefix(prefix string) retained.LocalScope {
	return routingTestScope{values: s.values, prefix: s.prefix + prefix + "/"}
}

func buildRoutingNode(t *testing.T, scope retained.LocalScope, model state.Model, dispatch func(update.Action), build func(*retained.Context[state.Model]) retained.ViewSpec[state.Model]) retained.RenderNode {
	t.Helper()

	root := retained.Build(build)
	node := retained.BuildViewRootNode(root, scope, dispatch, nil, func() state.Model { return model })
	if node == nil {
		t.Fatal("built node is nil")
	}
	return node
}

func executeEffects(t *testing.T, effects []update.Effect) {
	t.Helper()
	for _, eff := range effects {
		eff.Execute(context.Background())
	}
}

func noFocusAction(t *testing.T, actions []update.Action) {
	t.Helper()
	for _, action := range actions {
		if _, ok := action.(interaction.FocusKeyAction); ok {
			t.Fatalf("unexpected focus action in %#v", actions)
		}
	}
}

func findSetInteractionMode(t *testing.T, actions []update.Action) state.SetInteractionMode {
	t.Helper()
	for _, action := range actions {
		if mode, ok := action.(state.SetInteractionMode); ok {
			return mode
		}
	}
	t.Fatalf("no state.SetInteractionMode in %#v", actions)
	return state.SetInteractionMode{}
}

func TestBuildCreateRunOnRow_DeferredFocusDispatchesAfterCommit(t *testing.T) {
	t.Parallel()

	model := state.Model{
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			ProviderSpec: "openrouter",
		},
	}
	scope := routingTestScope{values: map[string]any{}}
	runPickerOpen := false
	pickerState := views.DefaultFilterablePickerState()
	dispatches := make([]update.Action, 0, 1)

	node := buildRoutingNode(t, scope, model, func(action update.Action) {
		dispatches = append(dispatches, action)
	}, func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildCreateRunOnRow(
			ctx,
			model.CreateDraftProviderConfig.ProviderSpec,
			model.CreateDraftProviderConfig.ProviderProtocol,
			"",
			runPickerOpen,
			func(next bool) { runPickerOpen = next },
			pickerState,
			func(next views.FilterablePickerState) { pickerState = next },
			func() {},
		)
	})

	handler, ok := node.(interaction.EventHandler)
	if !ok {
		t.Fatalf("node type = %T, want interaction.EventHandler", node)
	}
	actions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, nil)
	if !runPickerOpen {
		t.Fatal("run picker should be open after activation")
	}
	if got := findSetInteractionMode(t, actions); got.Mode != state.InteractionModePickOne {
		t.Fatalf("interaction mode = %q want %q", got.Mode, state.InteractionModePickOne)
	}
	noFocusAction(t, actions)

	dispatches = dispatches[:0]
	node = buildRoutingNode(t, scope, model, func(action update.Action) {
		dispatches = append(dispatches, action)
	}, func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildCreateRunOnRow(
			ctx,
			model.CreateDraftProviderConfig.ProviderSpec,
			model.CreateDraftProviderConfig.ProviderProtocol,
			"",
			runPickerOpen,
			func(next bool) { runPickerOpen = next },
			pickerState,
			func(next views.FilterablePickerState) { pickerState = next },
			func() {},
		)
	})
	hooks, ok := node.(interface{ PostCommitEffects() []update.Effect })
	if !ok {
		t.Fatalf("node type = %T, want PostCommitEffects provider", node)
	}
	executeEffects(t, hooks.PostCommitEffects())
	if len(dispatches) != 1 {
		t.Fatalf("dispatches len=%d want 1", len(dispatches))
	}
	focus, ok := dispatches[0].(interaction.FocusKeyAction)
	if !ok {
		t.Fatalf("dispatch[0]=%T want interaction.FocusKeyAction", dispatches[0])
	}
	if focus.Key != "openrouter" {
		t.Fatalf("focus key=%q want %q", focus.Key, "openrouter")
	}
}

func TestBuildRunOnWorkspaceRow_DeferredFocusDispatchesAfterCommit(t *testing.T) {
	t.Parallel()

	model := state.Model{
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "acme",
				SelectedProviderConfigRef: "backend-a",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{Ref: "backend-a"},
				},
			},
		},
	}
	scope := routingTestScope{values: map[string]any{}}
	dispatches := make([]update.Action, 0, 1)

	node := buildRoutingNode(t, scope, model, func(action update.Action) {
		dispatches = append(dispatches, action)
	}, func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return BuildRunOnWorkspaceRow(ctx)
	})

	handler, ok := node.(interaction.EventHandler)
	if !ok {
		t.Fatalf("node type = %T, want interaction.EventHandler", node)
	}
	actions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, nil)
	if got := findSetInteractionMode(t, actions); got.Mode != state.InteractionModePickOne {
		t.Fatalf("interaction mode = %q want %q", got.Mode, state.InteractionModePickOne)
	}
	noFocusAction(t, actions)

	dispatches = dispatches[:0]
	node = buildRoutingNode(t, scope, model, func(action update.Action) {
		dispatches = append(dispatches, action)
	}, func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return BuildRunOnWorkspaceRow(ctx)
	})
	hooks, ok := node.(interface{ PostCommitEffects() []update.Effect })
	if !ok {
		t.Fatalf("node type = %T, want PostCommitEffects provider", node)
	}
	executeEffects(t, hooks.PostCommitEffects())
	if len(dispatches) != 1 {
		t.Fatalf("dispatches len=%d want 1", len(dispatches))
	}
	focus, ok := dispatches[0].(interaction.FocusKeyAction)
	if !ok {
		t.Fatalf("dispatch[0]=%T want interaction.FocusKeyAction", dispatches[0])
	}
	if focus.Key != "backend-a" {
		t.Fatalf("focus key=%q want %q", focus.Key, "backend-a")
	}
}

func TestRunOnProviderChooseActions_ReselectionClosesWithoutSave(t *testing.T) {
	t.Parallel()

	snapshot := &state.EndpointSnapshot{
		Name:                      "acme",
		SelectedProviderConfigRef: "backend-a",
	}
	closeActions := []update.Action{
		state.SetInteractionMode{Mode: state.InteractionModeNAV},
		interaction.FocusKeyAction{Key: "run_on"},
	}
	got := primaryModelChooseActions(snapshot, "backend-a", func() []update.Action {
		return closeActions
	})

	if len(got) != len(closeActions) {
		t.Fatalf("action len=%d want=%d", len(got), len(closeActions))
	}
	for _, action := range got {
		switch action.(type) {
		case state.RoutingSaveStartedAction, state.SaveSelectedTargetRequested:
			t.Fatalf("unexpected save action for same provider: %T", action)
		}
	}
}

func TestRunOnProviderChooseActions_SelectionQueuesSaveThenClose(t *testing.T) {
	t.Parallel()

	snapshot := &state.EndpointSnapshot{
		Name:                      "acme",
		SelectedProviderConfigRef: "backend-a",
	}
	got := primaryModelChooseActions(snapshot, "backend-b", func() []update.Action {
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
			interaction.FocusKeyAction{Key: "run_on"},
		}
	})

	if len(got) != 4 {
		t.Fatalf("action len=%d want 4", len(got))
	}
	if _, ok := got[0].(state.RoutingSaveStartedAction); !ok {
		t.Fatalf("action[0]=%T want state.RoutingSaveStartedAction", got[0])
	}
	save, ok := got[1].(state.SaveSelectedTargetRequested)
	if !ok {
		t.Fatalf("action[1]=%T want state.SaveSelectedTargetRequested", got[1])
	}
	if save.EndpointName != "acme" || save.ProviderRef != "backend-b" {
		t.Fatalf("save action = %#v", save)
	}
}
