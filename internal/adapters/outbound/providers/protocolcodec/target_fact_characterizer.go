package protocolcodec

import (
	"context"
	"errors"
	"io"
	"strconv"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/thread"
	"github.com/swobuforge/swobu/internal/provider"
)

// CharacterizeTargetFact executes only the one-fact fixture owned by this
// codec. Preferred success proves true; a typed preferred rejection plus a
// successful control proves false. All other outcomes remain inconclusive.
func (c Codec) CharacterizeTargetFact(ctx context.Context, target provider.TargetSnapshot, fact provider.TargetFact, transport provider.Transport) provider.TargetFactResolution {
	request, ok := targetFactFixture(target.Model, fact)
	if !ok {
		return provider.TargetFactResolution{}
	}
	fixtureDelivery := targetFactFixtureDelivery(fact)
	threadID, err := thread.Derive("swobu/target-characterization/v1", target.TargetID, strconv.FormatUint(target.TargetVersion, 10), string(fact))
	if err != nil {
		return provider.TargetFactResolution{}
	}
	preferred := c.runTargetFactFixture(ctx, target, request, fixtureDelivery, threadID, fact, true, transport)
	if preferred == targetFactFixtureSucceeded {
		return provider.TargetFactResolution{Value: true, Conclusive: true}
	}
	if preferred != targetFactFixtureRejected {
		return provider.TargetFactResolution{}
	}
	if c.runTargetFactFixture(ctx, target, request, fixtureDelivery, threadID, fact, false, transport) == targetFactFixtureSucceeded {
		return provider.TargetFactResolution{Value: false, Conclusive: true}
	}
	return provider.TargetFactResolution{}
}

type targetFactFixtureOutcome uint8

const (
	targetFactFixtureInconclusive targetFactFixtureOutcome = iota
	targetFactFixtureSucceeded
	targetFactFixtureRejected
)

func (c Codec) runTargetFactFixture(ctx context.Context, target provider.TargetSnapshot, request canonical.CanonicalRequest, fixtureDelivery delivery.Delivery, threadID thread.ID, fact provider.TargetFact, value bool, transport provider.Transport) targetFactFixtureOutcome {
	facts := provider.NewTargetFacts(func(read provider.TargetFact) (bool, bool) {
		if read != fact {
			return true, false
		}
		return value, true
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		return targetFactFixtureInconclusive
	}
	providerRequest := provider.Request{
		Canonical: request, TargetFacts: facts, ToolNames: names,
		Delivery: fixtureDelivery,
		Attempt:  provider.AttemptContext{ThreadID: threadID},
	}
	document, _, err := c.Encode(providerRequest)
	reads := facts.Reads()
	if err != nil || len(reads) != 1 || reads[fact] != value {
		return targetFactFixtureInconclusive
	}
	ingress, err := transport.Send(ctx, document)
	if err != nil {
		failure, issued := provider.AsAttemptFailure(err)
		var rejected provider.RejectedError
		if issued && errors.As(failure.Cause(), &rejected) {
			return targetFactFixtureRejected
		}
		return targetFactFixtureInconclusive
	}
	decoded, err := c.Decode(ctx, providerRequest, ingress)
	if err != nil {
		return targetFactFixtureInconclusive
	}
	defer decoded.Stream.Close(ctx)
	for {
		_, err = decoded.Stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			return targetFactFixtureSucceeded
		}
		if err != nil {
			return targetFactFixtureInconclusive
		}
	}
}

func targetFactFixture(model string, fact provider.TargetFact) (canonical.CanonicalRequest, bool) {
	user, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("Reply with ok.")})
	if err != nil {
		return canonical.CanonicalRequest{}, false
	}
	params := canonical.RequestParams{Model: canonical.Specify(model), Items: []canonical.CanonicalItem{user}}
	switch fact {
	case provider.AcceptsParallelToolCallsFalse:
		key, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, "swobu_fact_fixture")
		if err != nil {
			return canonical.CanonicalRequest{}, false
		}
		schemaObject, err := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{}}`))
		if err != nil {
			return canonical.CanonicalRequest{}, false
		}
		schema := canonical.NewToolSchemaObject(schemaObject)
		tool, err := canonical.NewFunctionTool(key, "Fact fixture", schema, canonical.Unspecified[bool]())
		if err != nil {
			return canonical.CanonicalRequest{}, false
		}
		tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{tool})
		if err != nil {
			return canonical.CanonicalRequest{}, false
		}
		declarations, err := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeHistory)
		if err != nil {
			return canonical.CanonicalRequest{}, false
		}
		params.Items = []canonical.CanonicalItem{declarations, user}
		params.ToolCallBatch = canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne))
	case provider.AcceptsMaxCompletionTokens:
		limit := 1
		params.Controls, err = canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &limit})
	case provider.AcceptsReasoningEffortMax:
		effort := canonical.InferenceEffortMax
		params.Controls, err = canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	case provider.AcceptsReasoningDisabled:
		params.Reasoning, err = canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(canonical.NewDisabledReasoningCompute())})
	case provider.AcceptsFunctionCallOutputArray:
		key, keyErr := canonical.NewRequestToolKey(canonical.ToolKindFunction, "swobu_fact_fixture")
		if keyErr != nil {
			return canonical.CanonicalRequest{}, false
		}
		schemaObject, schemaErr := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{}}`))
		if schemaErr != nil {
			return canonical.CanonicalRequest{}, false
		}
		tool, toolErr := canonical.NewFunctionTool(key, "Fact fixture", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
		if toolErr != nil {
			return canonical.CanonicalRequest{}, false
		}
		tools, toolsErr := canonical.NewToolSet([]canonical.ToolDeclaration{tool})
		if toolsErr != nil {
			return canonical.CanonicalRequest{}, false
		}
		declarations, declarationsErr := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeHistory)
		if declarationsErr != nil {
			return canonical.CanonicalRequest{}, false
		}
		callID, callErr := canonical.NewToolCallID("swobu_fact_fixture_call")
		if callErr != nil {
			return canonical.CanonicalRequest{}, false
		}
		inputObject, inputErr := canonical.ParseJSONObject([]byte(`{}`))
		if inputErr != nil {
			return canonical.CanonicalRequest{}, false
		}
		call, callErr := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(inputObject))
		if callErr != nil {
			return canonical.CanonicalRequest{}, false
		}
		result, resultErr := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("ok")}, false)
		if resultErr != nil {
			return canonical.CanonicalRequest{}, false
		}
		params.Items = []canonical.CanonicalItem{declarations, user, call, result, user}
	case provider.AcceptsChatStreamIncludeUsage:
	default:
		return canonical.CanonicalRequest{}, false
	}
	if err != nil {
		return canonical.CanonicalRequest{}, false
	}
	return canonical.NewCanonicalRequest(params), true
}

func targetFactFixtureDelivery(fact provider.TargetFact) delivery.Delivery {
	if fact == provider.AcceptsChatStreamIncludeUsage {
		return delivery.StreamingDelivery(delivery.FramingSSE)
	}
	return delivery.BufferedDelivery()
}
