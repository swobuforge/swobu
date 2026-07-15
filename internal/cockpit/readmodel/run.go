package readmodel

// RunCommandID is the stable Cockpit identifier for a run affordance.
type RunCommandID string

// ClientID identifies the external client profile behind a run command.
type ClientID string

// RunCommandReadModel is the operator-facing affordance for launching a
// supported client against a workspace or route.
//
// It carries disclosure copy and the command preview as adapter-provided data;
// execution remains behind ports so renderers never shell out directly.
type RunCommandReadModel struct {
	ID             RunCommandID
	ClientID       ClientID
	Label          string
	CommandName    string
	TargetRouteID  RouteID
	TargetLabel    string
	Effect         RunCommandEffect
	CommandPreview string
}

// RunCommandEffect describes what will happen if the run command is confirmed.
type RunCommandEffect int

const (
	RunCommandOpensClient RunCommandEffect = iota
	RunCommandExecutesClient
	RunCommandPreparesClient
)

// DisclosureValue derives compact copy for run-once confirmation rows.
func (r RunCommandReadModel) DisclosureValue() string {
	target := r.TargetLabel
	if target == "" {
		target = string(r.TargetRouteID)
	}
	if target == "" {
		return r.CommandName
	}
	if r.CommandName == "" {
		return target
	}
	return r.CommandName + " -> " + target
}

// EffectLabel returns stable operator copy for the side effect class.
func (r RunCommandReadModel) EffectLabel() string {
	switch r.Effect {
	case RunCommandExecutesClient:
		return "executes client"
	case RunCommandPreparesClient:
		return "prepares client"
	default:
		return "opens client"
	}
}
