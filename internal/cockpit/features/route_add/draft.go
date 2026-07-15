package route_add

import (
	"fmt"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

const baseDraftModelName = "route-new"

// Draft owns the incomplete route row that exists only in the routes section.
type Draft struct {
	Open      *tui.State[bool]
	ModelName *tui.State[string]
}

func NewDraft() *Draft {
	return &Draft{
		Open:      tui.NewState(false),
		ModelName: tui.NewState(baseDraftModelName),
	}
}

func (d *Draft) IsOpen() bool {
	return d != nil && d.Open.Get()
}

func (d *Draft) OpenFor(existing []readmodel.RouteReadModel) {
	d.ModelName.Set(nextModelName(existing))
	d.Open.Set(true)
}

func (d *Draft) Close() {
	d.Open.Set(false)
}

func (d *Draft) Back() bool {
	if !d.IsOpen() {
		return false
	}
	d.Close()
	return true
}

func (d *Draft) Route(existing []readmodel.RouteReadModel) readmodel.RouteReadModel {
	modelName := normalizeModelName(d.ModelName.Get())
	if modelName == "" {
		modelName = nextModelName(existing)
	}
	return readmodel.RouteReadModel{
		ID:        readmodel.RouteID(modelName),
		ModelName: modelName,
		State:     readmodel.RouteIncomplete,
		PlanKind:  readmodel.RoutePlanSingle,
		Enabled:   true,
	}
}

func normalizeModelName(raw string) string {
	return strings.TrimSpace(raw) // swobu:io-string source=boundary
}

func nextModelName(existing []readmodel.RouteReadModel) string {
	used := map[string]struct{}{}
	for _, route := range existing {
		used[route.ModelName] = struct{}{}
		used[string(route.ID)] = struct{}{}
	}
	if _, ok := used[baseDraftModelName]; !ok {
		return baseDraftModelName
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", baseDraftModelName, i)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
}
