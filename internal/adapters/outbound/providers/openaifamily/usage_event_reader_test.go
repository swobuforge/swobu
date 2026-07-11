package openaifamily

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/report"
)

func normalizeUsageForTest(current canonical.TokenUsage) (canonical.TokenUsage, []report.Notice) {
	input, hasInput := current.InputTokens()
	if !hasInput {
		return current, nil
	}
	output, hasOutput := current.OutputTokens()
	if !hasOutput {
		return current, nil
	}
	normalized, err := canonical.NewTokenUsageWithOptional(&input, &output, nil, nil)
	if err != nil {
		return current, nil
	}
	return normalized, nil
}

type testProviderUsageDecoder struct{}

func (testProviderUsageDecoder) DecodeToCanonical(_ RawUsageEnvelope, current canonical.TokenUsage) (canonical.TokenUsage, []report.Notice) {
	return normalizeUsageForTest(current)
}
func TestUsageEventReader_RewritesUsageEventsOnly(t *testing.T) {
	input := 10
	output := 2
	cacheRead := 4
	cacheWrite := 1
	usage, err := canonical.NewTokenUsageWithOptional(&input, &output, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	now := time.Now().UTC()
	events := canonical.EventSequence{
		{ExchangeID: "ex", Seq: 1, Time: now, Kind: canonical.EventMetadata, EnvID: "r1", Payload: canonical.MetadataPayload{Values: map[string]string{"model": "m"}}},
		{ExchangeID: "ex", Seq: 2, Time: now, Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: usage}},
		{ExchangeID: "ex", Seq: 3, Time: now, Kind: canonical.EventFinish, EnvID: "r1", Payload: canonical.FinishPayload{Reason: "stop"}},
	}
	reader := newUsageEventReader(
		canonical.NewSliceEventReader(events),
		RawUsageEnvelope{ProviderID: profile.ProviderSpecOpenAI, Protocol: protocolkind.ChatCompletions},
		testProviderUsageDecoder{},
	)
	ev1, err := reader.Next(context.Background())
	if err != nil {
		t.Fatalf("next1: %v", err)
	}
	if ev1.Kind != canonical.EventMetadata {
		t.Fatalf("event1 kind=%s", ev1.Kind)
	}
	ev2, err := reader.Next(context.Background())
	if err != nil {
		t.Fatalf("next2: %v", err)
	}
	payload, ok := ev2.Payload.(canonical.UsagePayload)
	if !ok {
		t.Fatalf("usage payload type=%T", ev2.Payload)
	}
	if _, ok := payload.Usage.CacheReadTokens(); ok {
		t.Fatalf("cache read token should have been removed by normalization")
	}
	if _, ok := payload.Usage.CacheWriteTokens(); ok {
		t.Fatalf("cache write token should have been removed by normalization")
	}
	ev3, err := reader.Next(context.Background())
	if err != nil {
		t.Fatalf("next3: %v", err)
	}
	if ev3.Kind != canonical.EventFinish {
		t.Fatalf("event3 kind=%s", ev3.Kind)
	}
	if _, err := reader.Next(context.Background()); err != io.EOF {
		t.Fatalf("eof err=%v", err)
	}
}
