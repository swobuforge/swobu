package wire

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestResolveLoweredToolPolicy(t *testing.T) {
	functionKey, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	if err != nil {
		t.Fatal(err)
	}
	missingKey, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, "missing")
	if err != nil {
		t.Fatal(err)
	}
	zero := LoweredToolSet{Records: []LoweredToolRecord{{Key: functionKey, Kind: canonical.ToolKindFunction}}}
	one := LoweredToolSet{Records: []LoweredToolRecord{{Key: functionKey, Kind: canonical.ToolKindFunction, FragmentCount: 1}}}
	many := LoweredToolSet{Records: []LoweredToolRecord{{Key: functionKey, Kind: canonical.ToolKindFunction, FragmentCount: 2}}}

	for _, tc := range []struct {
		name             string
		policy           canonical.ToolPolicy
		lowered          LoweredToolSet
		wantRecord       bool
		wantBadRequest   bool
		wantIncompatible bool
	}{
		{name: "none", policy: canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), lowered: zero},
		{name: "auto", policy: canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), lowered: zero},
		{name: "required survives", policy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), lowered: one},
		{name: "required undeclared", policy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), wantBadRequest: true},
		{name: "required omitted", policy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), lowered: zero, wantIncompatible: true},
		{name: "specific undeclared", policy: canonical.NewToolPolicy(canonical.ToolPolicySpecific, &missingKey), lowered: one, wantBadRequest: true},
		{name: "specific omitted", policy: canonical.NewToolPolicy(canonical.ToolPolicySpecific, &functionKey), lowered: zero, wantIncompatible: true},
		{name: "specific exact", policy: canonical.NewToolPolicy(canonical.ToolPolicySpecific, &functionKey), lowered: one, wantRecord: true},
		{name: "specific expands", policy: canonical.NewToolPolicy(canonical.ToolPolicySpecific, &functionKey), lowered: many, wantIncompatible: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record, err := ResolveLoweredToolPolicy(tc.policy, tc.lowered)
			var incompatible provider.IncompatibleTargetError
			var canonicalError canonical.Error
			if tc.wantIncompatible != errors.As(err, &incompatible) {
				t.Fatalf("incompatible = %t, error = %T %v", tc.wantIncompatible, err, err)
			}
			badRequest := errors.As(err, &canonicalError) && canonicalError.Code == canonical.ErrorCodeBadRequest
			if tc.wantBadRequest != badRequest {
				t.Fatalf("bad request = %t, error = %T %v", tc.wantBadRequest, err, err)
			}
			if (record != nil) != tc.wantRecord {
				t.Fatalf("record = %#v, want present=%t", record, tc.wantRecord)
			}
		})
	}
}
