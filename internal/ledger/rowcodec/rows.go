// Package rowcodec contains the portable, database-neutral shape used while
// reconstructing persisted Ledger invocations. SQL adapters own scanning and
// placeholder syntax; this package owns the closed-kind invariants.
package rowcodec

import (
	"errors"
	"strconv"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

type Invocation struct {
	Kind                                                       string
	Route, Target, Version, Provider, Protocol, Model, Purpose string
	SourceTool, Tool                                           string
	Input, Output, Reasoning, CacheRead, CacheWrite            *int64
}

func (r Invocation) Validate() error {
	switch r.Kind {
	case "model":
		if r.Target == "" || r.Version == "" || r.Provider == "" || r.Protocol == "" || r.Model == "" || r.Purpose == "" || r.SourceTool != "" || r.Tool != "" {
			return errors.New("persisted model Invocation subtype is incomplete")
		}
	case "mcp_tool":
		if r.SourceTool == "" || r.Tool == "" || r.Route != "" || r.Target != "" || r.Version != "" || r.Provider != "" || r.Protocol != "" || r.Model != "" || r.Purpose != "" {
			return errors.New("persisted MCP Invocation subtype is incomplete")
		}
	default:
		return errors.New("persisted Invocation kind is invalid")
	}
	return nil
}

// Restore reconstructs one persisted invocation identically for every SQL
// adapter. Adapters only translate driver nulls and scan columns into Row.
func (r Invocation) Restore(id execution.InvocationID, startedAt, settledAt time.Time, duration time.Duration, outcome execution.Outcome, occurrence execution.Occurrence, failure string) (executionevidence.InvocationLifecycle, error) {
	if err := r.Validate(); err != nil {
		return executionevidence.InvocationLifecycle{}, err
	}
	switch r.Kind {
	case "model":
		var route routing.RouteName
		var err error
		if r.Route != "" {
			route, err = routing.ParseRouteName(r.Route)
			if err != nil {
				return executionevidence.InvocationLifecycle{}, err
			}
		}
		target, err := routing.ParseTargetID(r.Target)
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		version, err := strconv.ParseUint(r.Version, 10, 64)
		if err != nil || version == 0 {
			return executionevidence.InvocationLifecycle{}, errors.New("persisted TargetVersion is invalid")
		}
		protocol, err := protocolkind.ParseProtocolKind(r.Protocol)
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		model, err := routing.ParseUpstreamModel(r.Model)
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		resource, err := executionevidence.NewModelResource(route, target, routing.TargetVersion(version), routing.Provider(r.Provider), protocol, model, executionevidence.ModelPurpose(r.Purpose))
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		start, err := executionevidence.NewModelInvocationStarted(id, startedAt, resource)
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: intPtr(r.Input), OutputTokens: intPtr(r.Output), ReasoningTokens: intPtr(r.Reasoning), CacheReadTokens: intPtr(r.CacheRead), CacheWriteTokens: intPtr(r.CacheWrite)})
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		settled, err := executionevidence.NewModelInvocationSettled(id, settledAt, outcome, occurrence, executionevidence.ModelFailure(failure), usage)
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		return executionevidence.RestoreInvocationLifecycle(start, settled, duration)
	case "mcp_tool":
		source, err := canonical.ParseToolKey(r.SourceTool)
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		tool, err := canonical.ParseToolKey(r.Tool)
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		resource, err := executionevidence.NewMCPResource(source, tool)
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		start, err := executionevidence.NewMCPInvocationStarted(id, startedAt, resource)
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		settled, err := executionevidence.NewMCPInvocationSettled(id, settledAt, outcome, occurrence, executionevidence.MCPFailure(failure))
		if err != nil {
			return executionevidence.InvocationLifecycle{}, err
		}
		return executionevidence.RestoreInvocationLifecycle(start, settled, duration)
	default:
		return executionevidence.InvocationLifecycle{}, errors.New("persisted Invocation kind is invalid")
	}
}

func intPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}
