package otlp

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	apitrace "go.opentelemetry.io/otel/trace"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestProjectProducesContentFreeHistoricalTraceShape(t *testing.T) {
	exchangeID, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := routing.ParseWorkspaceSlug("acme")
	startedAt := time.Unix(100, 123)
	settledAt := time.Unix(104, 456)
	started, err := executionevidence.NewExchangeStarted(exchangeID, accessidentity.Local, workspace, startedAt, clientfamily.Codex, protocolkind.Responses, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := routing.ParseRouteName("chat")
	target, _ := routing.ParseTargetID("target-a")
	providerID, _ := routing.ParseProvider("openai", func(string) bool { return true })
	model, _ := routing.ParseUpstreamModel("gpt-test")
	modelResource, _ := executionevidence.NewModelResource(route, target, routing.TargetVersion(42), providerID, protocolkind.Responses, model, executionevidence.ExchangeAttempt)
	connectionID, _ := connectionidentity.Parse("cn_trace")
	modelResource, err = modelResource.BindConnection(connectionID, ^uint64(0))
	if err != nil {
		t.Fatal(err)
	}
	modelID, _ := execution.NewInvocationID(exchangeID, 1)
	modelStart, _ := executionevidence.NewModelInvocationStarted(modelID, time.Unix(101, 1), modelResource)
	modelSettle, _ := executionevidence.NewModelInvocationSettled(modelID, time.Unix(102, 2), execution.Failed, execution.Occurred, executionevidence.FailureTimeout, canonical.NewUnknownTokenUsage())
	modelLifecycle, _ := executionevidence.NewInvocationLifecycle(modelStart, modelSettle)
	tool, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "search")
	mcpResource, _ := executionevidence.NewMCPResource(tool, tool)
	mcpID, _ := execution.NewInvocationID(exchangeID, 2)
	mcpStart, _ := executionevidence.NewMCPInvocationStarted(mcpID, time.Unix(102, 3), mcpResource)
	mcpSettle, _ := executionevidence.NewMCPInvocationSettled(mcpID, time.Unix(103, 4), execution.Succeeded, execution.Occurred, "")
	mcpLifecycle, _ := executionevidence.NewInvocationLifecycle(mcpStart, mcpSettle)
	exchangeSettle, _ := executionevidence.NewExchangeSettled(exchangeID, settledAt, delivery.Succeeded, 200, "")
	completed, err := executionevidence.NewCompletedExchange(started, []executionevidence.InvocationLifecycle{modelLifecycle, mcpLifecycle}, exchangeSettle)
	if err != nil {
		t.Fatal(err)
	}

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithResource(resource.Default()), sdktrace.WithSpanProcessor(recorder))
	e := &Exporter{provider: provider}
	if err := e.project(context.Background(), completed); err != nil {
		t.Fatal(err)
	}
	spans := recorder.Ended()
	if len(spans) != 3 {
		t.Fatalf("ended spans = %d, want root plus two children", len(spans))
	}
	bindingFound := false
	for _, span := range spans {
		values := map[string]string{}
		for _, attr := range span.Attributes() {
			values[string(attr.Key)] = attr.Value.AsString()
		}
		if values["swobu.connection.id"] == "cn_trace" {
			bindingFound = values["swobu.connection.revision"] == "18446744073709551615"
		}
	}
	if !bindingFound {
		t.Fatal("trace lost exact Connection revision")
	}
	var root sdktrace.ReadOnlySpan
	for _, span := range spans {
		if span.Parent().IsValid() == false {
			root = span
		}
	}
	if root == nil {
		t.Fatal("missing local root span")
	}
	if root.SpanKind() != apitrace.SpanKindServer {
		t.Fatalf("root kind = %v, want server", root.SpanKind())
	}
	if !root.StartTime().Equal(startedAt) || !root.EndTime().Equal(settledAt) {
		t.Fatalf("root timing = %s..%s", root.StartTime(), root.EndTime())
	}
	if root.Status().Code != 0 {
		t.Fatalf("root status = %v, want unset for successful delivery", root.Status().Code)
	}
	for _, span := range spans {
		if span == root {
			continue
		}
		if !span.Parent().IsValid() || span.Parent().SpanID() != root.SpanContext().SpanID() {
			t.Fatal("child is not rooted at exchange span")
		}
		if span.SpanKind() != apitrace.SpanKindClient {
			t.Fatalf("child kind = %v, want client", span.SpanKind())
		}
		if span.Name() == "" || !span.StartTime().Before(span.EndTime()) {
			t.Fatal("child has invalid timing")
		}
		if span.Name() != "swobu invocation" {
			t.Fatalf("child name = %q", span.Name())
		}
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "swobu.execution.outcome" && attr.Value.AsString() == string(execution.Failed) && span.Status().Code != 1 {
				t.Fatalf("failed invocation status = %v", span.Status().Code)
			}
		}
		for _, attr := range span.Attributes() {
			if strings.Contains(strings.ToLower(string(attr.Key)), "prompt") || strings.Contains(strings.ToLower(string(attr.Key)), "completion") || strings.Contains(strings.ToLower(string(attr.Key)), "content") || strings.Contains(strings.ToLower(string(attr.Key)), "argument") || strings.Contains(strings.ToLower(string(attr.Key)), "result") {
				t.Fatalf("prohibited content attribute %q", attr.Key)
			}
		}
	}
	if got := e.exported.Load(); got != 1 {
		t.Fatalf("exported = %d, want 1", got)
	}
	_ = provider.Shutdown(context.Background())
}
