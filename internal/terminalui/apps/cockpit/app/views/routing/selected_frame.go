package routing

import (
	"strings"

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
		_ = ctx
		return buildProviderFrameChoiceRow(spec)
	})
}

func buildProviderFrameChoiceRow(spec providerFrameChoiceRowSpec) retained.ViewSpec[state.Model] {
	if spec.ProviderConfig == nil {
		return views.RowStatic(providerDeliveryRowLabel, "not set")
	}
	frames := providercatalog.SupportedFramesForSpecProtocol(
		spec.ProviderConfig.ProviderSpec,
		protocolkind.ProtocolKind(strings.TrimSpace(spec.ProviderConfig.ProtocolKind)), // trimlowerlint:allow boundary canonicalization
	)
	selected := strings.TrimSpace(spec.ProviderConfig.SelectedFrame) // trimlowerlint:allow boundary canonicalization
	if selected == "" && len(frames) > 0 {
		selected = frames[0]
	}
	if selected == "" {
		selected = "not set"
	}
	protocol := protocolkind.ProtocolKind(strings.TrimSpace(spec.ProviderConfig.ProtocolKind)) // trimlowerlint:allow boundary canonicalization
	return views.RowActionWithCancel(providerDeliveryRowLabel, presentDeliveryFrameForProvider(spec.ProviderConfig.ProviderSpec, protocol, selected), "next", func() []update.Action {
		next := nextFrameSelection(frames, strings.TrimSpace(spec.ProviderConfig.SelectedFrame)) // trimlowerlint:allow boundary canonicalization
		if next == "" {
			return nil
		}
		if spec.CreateMode {
			return []update.Action{state.SetCreateDraftSelectedFrame{SelectedFrame: next}}
		}
		if strings.TrimSpace(spec.EndpointName) == "" { // trimlowerlint:allow boundary canonicalization
			return nil
		}
		updated := *spec.ProviderConfig
		updated.SelectedFrame = next
		return routingSaveProviderConfigActions(strings.TrimSpace(spec.EndpointName), updated, "provider/frame") // trimlowerlint:allow boundary canonicalization
	}, nil)
}

func nextFrameSelection(frames []string, current string) string {
	if len(frames) == 0 {
		return ""
	}
	current = strings.TrimSpace(current) // trimlowerlint:allow boundary canonicalization
	for i, frame := range frames {
		if frame != current {
			continue
		}
		return frames[(i+1)%len(frames)]
	}
	return frames[0]
}
