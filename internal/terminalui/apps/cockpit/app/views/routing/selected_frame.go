package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type providerFrameChoiceRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerFrameChoiceRow(spec providerFrameChoiceRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderFrameChoiceRow(ctx.Model(), spec)
	})
}

func buildProviderFrameChoiceRow(model state.Model, spec providerFrameChoiceRowSpec) retained.ViewSpec[state.Model] {
	if spec.ProviderConfig == nil {
		return views.RowStatic(providerDeliveryRowLabel, "not set")
	}
	frames := providercatalog.SupportedFramesForSpecProtocol(
		spec.ProviderConfig.ProviderSpec,
		protocolkind.ProtocolKind(strings.TrimSpace(spec.ProviderConfig.ProtocolKind)), // swobu:io-string source=boundary
	)
	selected := strings.TrimSpace(spec.ProviderConfig.SelectedFrame) // swobu:io-string source=boundary
	if selected == "" && len(frames) > 0 {
		selected = frames[0]
	}
	if selected == "" {
		selected = "not set"
	}
	summary := "auto"
	if spec.ProviderConfig.ModelID != "" {
		protocol := protocolkind.ProtocolKind(strings.TrimSpace(spec.ProviderConfig.ProtocolKind)) // swobu:io-string source=boundary
		summary = presentDeliveryFrameForProvider(spec.ProviderConfig.ProviderSpec, protocol, selected)
	}
	return views.RowActionWithHooks(providerDeliveryRowLabel, summary, "next", func() []update.Action {
		if spec.CreateMode {
			if actions, ok := firstRunCreateFromDeliveryActions(model); ok {
				return actions
			}
		}
		next := nextFrameSelection(frames, strings.TrimSpace(spec.ProviderConfig.SelectedFrame)) // swobu:io-string source=boundary
		if next == "" {
			return nil
		}
		if spec.CreateMode {
			return []update.Action{state.SetCreateDraftSelectedFrame{SelectedFrame: next}}
		}
		if strings.TrimSpace(spec.EndpointName) == "" { // swobu:io-string source=boundary
			return nil
		}
		updated := *spec.ProviderConfig
		updated.SelectedFrame = next
		return routingSaveProviderConfigActions(strings.TrimSpace(spec.EndpointName), updated, "provider/frame") // swobu:io-string source=boundary
	}, nil, views.FocusAffordance("next", false))
}

func firstRunCreateFromDeliveryActions(model state.Model) ([]update.Action, bool) {
	name := model.CreateDraftName
	if name == "" {
		return nil, false
	}
	parsed, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		return nil, false
	}
	flow := state.EvaluateCreateDraftRouteSetup(model.CreateDraftProviderConfig)
	if !flow.Ready {
		return nil, false
	}
	canonicalName := parsed.String()
	return []update.Action{
		state.SetCreateDraftName{Name: canonicalName},
		state.WorkspaceCreateRequested{Name: canonicalName},
	}, true
}

func nextFrameSelection(frames []string, current string) string {
	if len(frames) == 0 {
		return ""
	}
	current = strings.TrimSpace(current) // swobu:io-string source=boundary
	for i, frame := range frames {
		if frame != current {
			continue
		}
		return frames[(i+1)%len(frames)]
	}
	return frames[0]
}
