package evidence

import (
	"testing"
)

func TestTrafficEvent_ClonesAdaptationChain(t *testing.T) {
	requestID, err := ParseRequestID("req-1")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "m")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	inputTokens := 100
	outputTokens := 7
	reasoningTokens := 11
	cacheReadTokens := 64
	cacheWriteTokens := 4
	usage, err := NewTokenUsage(TokenUsageParams{
		InputTokens:      &inputTokens,
		OutputTokens:     &outputTokens,
		ReasoningTokens:  &reasoningTokens,
		CacheReadTokens:  &cacheReadTokens,
		CacheWriteTokens: &cacheWriteTokens,
	})
	if err != nil {
		t.Fatalf("NewTokenUsage returned error: %v", err)
	}

	chain := []string{"bridge", "responses"}
	mutations := []Mutation{{
		Stage:         "encode",
		PatchID:       "p.encode",
		Changed:       true,
		ChangedFields: []string{"prompt_cache_key"},
	}}
	diagnostics := []string{"high_patch_noop_ratio:4/5"}
	stageReports := []StageReport{{
		Stage:   "provider.wire.out",
		Carrier: "wire_document",
		Applied: []string{"p.encode"},
		Mutated: true,
	}}
	event, err := NewTerminalTrafficEvent(TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		ClientProtocol:      "openai_compat",
		ClientHandler:       "codex",
		ClientFamily:        "chat_completions",
		NormalizedOp:        "/chat/completions",
		Route:               route,
		AdaptationChain:     chain,
		Result:              ResultClassSuccess,
		StatusCode:          200,
		TokenUsage:          usage,
		ModelRequested:      "client-model",
		ModelResolved:       "resolved-model",
		ModelResolutionMode: "default_missing",
		Mutations:           mutations,
		ExchangeDiagnostics: diagnostics,
		StageReports:        stageReports,
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}
	chain[0] = "mutated"
	if got := event.AdaptationChain()[0]; got != "bridge" {
		t.Fatalf("adaptation chain[0] = %q, want %q", got, "bridge")
	}
	if got := event.ModelRequested(); got != "client-model" {
		t.Fatalf("model requested = %q, want %q", got, "client-model")
	}
	if got := event.ModelResolved(); got != "resolved-model" {
		t.Fatalf("model resolved = %q, want %q", got, "resolved-model")
	}
	if got := event.ModelResolutionMode(); got != "default_missing" {
		t.Fatalf("model resolution mode = %q, want %q", got, "default_missing")
	}
	if got := event.ClientProtocol(); got != "openai_compat" {
		t.Fatalf("client protocol = %q, want %q", got, "openai_compat")
	}
	if got := event.ClientHandler(); got != "codex" {
		t.Fatalf("client handler = %q, want %q", got, "codex")
	}
	if got := event.ClientFamily(); got != "chat_completions" {
		t.Fatalf("ingress family = %q, want %q", got, "chat_completions")
	}
	if got := event.NormalizedOp(); got != "/chat/completions" {
		t.Fatalf("normalized op = %q, want %q", got, "/chat/completions")
	}
	if got, ok := event.TokenUsage().InputTokens(); !ok || got != 100 {
		t.Fatalf("token usage input = (%d,%v), want (100,true)", got, ok)
	}
	if got, ok := event.TokenUsage().OutputTokens(); !ok || got != 7 {
		t.Fatalf("token usage output = (%d,%v), want (7,true)", got, ok)
	}
	if got, ok := event.TokenUsage().ReasoningTokens(); !ok || got != 11 {
		t.Fatalf("token usage reasoning = (%d,%v), want (11,true)", got, ok)
	}
	if got, ok := event.TokenUsage().CacheReadTokens(); !ok || got != 64 {
		t.Fatalf("token usage cache read = (%d,%v), want (64,true)", got, ok)
	}
	if got, ok := event.TokenUsage().CacheWriteTokens(); !ok || got != 4 {
		t.Fatalf("token usage cache write = (%d,%v), want (4,true)", got, ok)
	}
	mutations[0].ChangedFields[0] = "mutated"
	gotMutations := event.Mutations()
	if len(gotMutations) != 1 || gotMutations[0].ChangedFields[0] != "prompt_cache_key" {
		t.Fatalf("wire patch mutations = %#v", gotMutations)
	}
	diagnostics[0] = "mutated"
	gotDiagnostics := event.ExchangeDiagnostics()
	if len(gotDiagnostics) != 1 || gotDiagnostics[0] != "high_patch_noop_ratio:4/5" {
		t.Fatalf("exchange diagnostics = %#v", gotDiagnostics)
	}
	stageReports[0].Applied[0] = "mutated"
	gotStageReports := event.StageReports()
	if len(gotStageReports) != 1 || gotStageReports[0].Applied[0] != "p.encode" {
		t.Fatalf("exchange stage reports = %#v", gotStageReports)
	}
}

func TestTrafficEvent_RejectsTerminalInProgressResult(t *testing.T) {
	requestID, err := ParseRequestID("req-1")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	if _, err := NewTerminalTrafficEvent(TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		Route:      route,
		Result:     ResultClassInProgress,
		StatusCode: 200,
	}); err == nil {
		t.Fatal("terminal events should reject in_progress result class")
	}
}

func TestTiming_RejectsDurationBeforeTTFB(t *testing.T) {
	ttfb := 50
	dur := 10
	if _, err := NewTimingWithOptional(&ttfb, &dur); err == nil {
		t.Fatal("NewTimingWithOptional should reject duration before ttfb")
	}
}

func TestTrafficEvent_RejectsDuplicateStageReports(t *testing.T) {
	requestID, err := ParseRequestID("req-dup-stage")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	_, err = NewTerminalTrafficEvent(TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		Route:      route,
		Result:     ResultClassSuccess,
		StatusCode: 200,
		StageReports: []StageReport{
			{Stage: "provider.wire.out", Carrier: "wire_document", Applied: []string{"p.a"}, Mutated: true},
			{Stage: "provider.wire.out", Carrier: "wire_document", Applied: []string{"p.b"}, Mutated: true},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate stage report error, got nil")
	}
}

func TestTrafficEvent_RejectsMutatedStageReportWithoutAppliedPatches(t *testing.T) {
	requestID, err := ParseRequestID("req-mut-stage")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	_, err = NewTerminalTrafficEvent(TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		Route:      route,
		Result:     ResultClassSuccess,
		StatusCode: 200,
		StageReports: []StageReport{
			{Stage: "provider.wire.out", Carrier: "wire_document", Mutated: true},
		},
	})
	if err == nil {
		t.Fatal("expected mutated-without-applied error, got nil")
	}
}

func TestTrafficEvent_RejectsStageReportWithEmptyStage(t *testing.T) {
	requestID, err := ParseRequestID("req-empty-stage")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	_, err = NewTerminalTrafficEvent(TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		Route:      route,
		Result:     ResultClassSuccess,
		StatusCode: 200,
		StageReports: []StageReport{
			{Stage: " ", Carrier: "wire_document", Applied: []string{"p.a"}, Mutated: true},
		},
	})
	if err == nil {
		t.Fatal("expected empty stage report error, got nil")
	}
}

func TestTrafficEvent_RejectsStageReportWithEmptyCarrier(t *testing.T) {
	requestID, err := ParseRequestID("req-empty-carrier")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	_, err = NewTerminalTrafficEvent(TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		Route:      route,
		Result:     ResultClassSuccess,
		StatusCode: 200,
		StageReports: []StageReport{
			{Stage: "provider.wire.out", Carrier: " ", Applied: []string{"p.a"}, Mutated: true},
		},
	})
	if err == nil {
		t.Fatal("expected empty carrier report error, got nil")
	}
}

func TestTrafficEvent_AccessorsDeepCloneNestedMetadataSlices(t *testing.T) {
	requestID, err := ParseRequestID("req-deep-clone")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	event, err := NewTerminalTrafficEvent(TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		Route:      route,
		Result:     ResultClassSuccess,
		StatusCode: 200,
		Mutations: []Mutation{{
			Stage:         "encode",
			PatchID:       "p.encode",
			Changed:       true,
			ChangedFields: []string{"f1"},
		}},
		StageReports: []StageReport{{
			Stage:   "provider.wire.out",
			Carrier: "wire_document",
			Applied: []string{"p.encode"},
			Mutated: true,
		}},
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}

	mutations := event.Mutations()
	mutations[0].ChangedFields[0] = "mutated"
	stageReports := event.StageReports()
	stageReports[0].Applied[0] = "mutated"

	refetchedMutations := event.Mutations()
	if refetchedMutations[0].ChangedFields[0] != "f1" {
		t.Fatalf("wire patch changed_fields mutated through accessor: %#v", refetchedMutations)
	}
	refetchedStages := event.StageReports()
	if refetchedStages[0].Applied[0] != "p.encode" {
		t.Fatalf("stage report applied mutated through accessor: %#v", refetchedStages)
	}
}

func TestTrafficEvent_NormalizesStageReportCaseWhitespaceAndAppliedOrdering(t *testing.T) {
	requestID, err := ParseRequestID("req-stage-normalize")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	event, err := NewTerminalTrafficEvent(TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		Route:      route,
		Result:     ResultClassSuccess,
		StatusCode: 200,
		StageReports: []StageReport{
			{
				Stage:   " Provider.Wire.Out ",
				Carrier: " Wire_Document ",
				Applied: []string{" z.patch ", "a.patch", "a.patch"},
				Mutated: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}
	reports := event.StageReports()
	if len(reports) != 1 {
		t.Fatalf("reports len=%d want 1 (%#v)", len(reports), reports)
	}
	if reports[0].Stage != "provider.wire.out" {
		t.Fatalf("stage=%q want provider.wire.out", reports[0].Stage)
	}
	if reports[0].Carrier != "wire_document" {
		t.Fatalf("carrier=%q want wire_document", reports[0].Carrier)
	}
	if len(reports[0].Applied) != 2 || reports[0].Applied[0] != "a.patch" || reports[0].Applied[1] != "z.patch" {
		t.Fatalf("applied=%#v want [a.patch z.patch]", reports[0].Applied)
	}
}

func TestTrafficEvent_DetectsDuplicateStageReportsAfterNormalization(t *testing.T) {
	requestID, err := ParseRequestID("req-stage-dup-normalized")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	_, err = NewTerminalTrafficEvent(TrafficEventInput{RequestID: requestID, Endpoint: "alpha",
		Route:      route,
		Result:     ResultClassSuccess,
		StatusCode: 200,
		StageReports: []StageReport{
			{Stage: "provider.wire.out", Carrier: "wire_document", Applied: []string{"p.a"}, Mutated: true},
			{Stage: " Provider.Wire.Out ", Carrier: " Wire_Document ", Applied: []string{"p.b"}, Mutated: true},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate stage report error after normalization, got nil")
	}
}
