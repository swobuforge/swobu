package replay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/conformance/fixture"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

type fixtureEvent struct {
	Seq     int64  `json:"seq"`
	Kind    string `json:"kind"`
	EnvKind string `json:"env_kind"`
	Status  string `json:"status"`
	Text    string `json:"text"`
	EnvID   string `json:"env_id"`
	Parent  string `json:"parent"`
}

func TestExchangeReplay(t *testing.T) {
	t.Parallel()
	root := repoRootFromHere(t)
	base := filepath.Join(root, "testdata", "exchange")

	for _, caseDirName := range fixture.RequiredCases {
		caseDirName := caseDirName
		t.Run(caseDirName, func(t *testing.T) {
			t.Parallel()
			caseDir := filepath.Join(base, caseDirName)
			assertDirExists(t, caseDir)
			for _, file := range fixture.RequiredFiles {
				path := filepath.Join(caseDir, file)
				assertFileHasContent(t, path)
			}

			contract := mustLoadCaseContract(t, filepath.Join(caseDir, "case.yaml"))
			if strings.TrimSpace(contract.Name) != caseDirName {
				t.Fatalf("case contract name=%q want %q", contract.Name, caseDirName)
			}
			assertCaseContract(t, contract)

			assertEnvelopeProjectionFromCanonicalEvents(t, filepath.Join(caseDir, "canonical_events.jsonl"), filepath.Join(caseDir, "client_response.body.json"))
			assertRunnerReplay(t, contract, caseDir)
			assertDeliveryConversionInvariants(t, contract, caseDir)
		})
	}
}

func assertRunnerReplay(t *testing.T, contract fixture.CaseContract, caseDir string) {
	t.Helper()
	clientFamily, providerFamily := mustMapFamilies(t, contract)
	clientDelivery, providerDelivery := mustMapDeliveries(t, contract)
	clientRequest := []byte(readFile(t, filepath.Join(caseDir, "client_request.body.json")))
	providerResponseBody := []byte(readFile(t, filepath.Join(caseDir, "provider_response.body.json")))
	providerResponseSSE := readFile(t, filepath.Join(caseDir, "provider_response.sse"))
	requestDecoder, err := protocolregistry.ForClientRequestDecoder(clientFamily)
	if err != nil {
		t.Fatalf("client request decoder: %v", err)
	}
	request, _, err := requestDecoder.DecodeClientRequest(carrier.WireDocument{
		Leg:    carrier.LegClientRequestIn,
		Family: protocolkind.ProtocolKind(clientFamily),
		Media:  "application/json",
		Raw:    append([]byte(nil), clientRequest...),
	})
	if err != nil {
		t.Fatalf("decode client request: %v", err)
	}
	var capturedProviderRequest []byte
	runner := exchange.Runner{ProviderExecute: func(_ context.Context, req exchange.ProviderRequest) (exchange.ProviderTransportResponse, error) {
		capturedProviderRequest = append(capturedProviderRequest[:0], req.ProviderWire.Raw...)
		if providerDelivery.Mode == delivery.Streaming {
			return exchange.ProviderTransportResponse{
				Stream: io.NopCloser(strings.NewReader(providerResponseSSE)),
			}, nil
		}
		return exchange.ProviderTransportResponse{
			Document: providerResponseBody,
		}, nil
	}}
	out, err := runner.Run(context.Background(), withRuntimeInput(exchange.ClientInput{
		ExchangeID:       "fixture_exchange",
		ClientFamily:     clientFamily,
		ClientDelivery:   clientDelivery,
		Request:          request,
		ProviderFamily:   providerFamily,
		ProviderDelivery: providerDelivery,
		Target:           exchange.NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", providerFamily, "", "", ""),
		Contract:         exchange.NewExecutionContract(clientDelivery).WithProviderDelivery(providerDelivery),
	}))
	if err != nil {
		t.Fatalf("runner replay failed: %v", err)
	}
	assertJSONEqual(t, capturedProviderRequest, []byte(readFile(t, filepath.Join(caseDir, "provider_request.body.json"))), "provider request")
	if clientDelivery.Mode == delivery.Streaming {
		if out.Stream == nil {
			t.Fatalf("client stream missing")
		}
		streamRaw, readErr := io.ReadAll(carrier.ReadCloserFromFrameReader(out.Stream.Frames))
		if readErr != nil {
			t.Fatalf("read client stream: %v", readErr)
		}
		got := strings.TrimSpace(string(streamRaw))
		if !strings.Contains(got, "data: ") || !containsStreamTerminalMarker(got) {
			t.Fatalf("client stream missing semantic markers: %s", got)
		}
	} else {
		if out.Document == nil {
			t.Fatalf("buffered client document missing")
		}
		if !strings.Contains(strings.ToLower(string(out.Document.Raw)), "ok") {
			t.Fatalf("client response missing expected semantic text: %s", string(out.Document.Raw))
		}
	}
}

func mustMapFamilies(t *testing.T, contract fixture.CaseContract) (canonical.IngressFamily, protocolkind.ProtocolKind) {
	t.Helper()
	var clientFamily canonical.IngressFamily
	switch strings.TrimSpace(contract.Client.Family) {
	case "chatcompletions":
		clientFamily = canonical.IngressFamilyChatCompletions
	case "responses":
		clientFamily = canonical.IngressFamilyResponses
	case "messages":
		clientFamily = canonical.IngressFamilyMessages
	case "completions":
		clientFamily = canonical.IngressFamilyCompletions
	default:
		t.Fatalf("unsupported client family %q", contract.Client.Family)
	}
	var providerFamily protocolkind.ProtocolKind
	switch strings.TrimSpace(contract.Provider.Family) {
	case "chatcompletions":
		providerFamily = protocolkind.ChatCompletions
	case "responses":
		providerFamily = protocolkind.Responses
	case "messages":
		providerFamily = protocolkind.Messages
	case "completions":
		providerFamily = protocolkind.Completions
	default:
		t.Fatalf("unsupported provider family %q", contract.Provider.Family)
	}
	return clientFamily, providerFamily
}

func mustMapDeliveries(t *testing.T, contract fixture.CaseContract) (delivery.Delivery, delivery.Delivery) {
	t.Helper()
	toDelivery := func(raw string) delivery.Delivery {
		if strings.TrimSpace(raw) == "streaming" {
			return delivery.StreamingDelivery(delivery.FramingSSE)
		}
		return delivery.BufferedDelivery()
	}
	return toDelivery(contract.Client.Delivery), toDelivery(contract.Provider.Delivery)
}

func assertJSONEqual(t *testing.T, got []byte, want []byte, label string) {
	t.Helper()
	var gotObj any
	var wantObj any
	if err := json.Unmarshal(got, &gotObj); err != nil {
		t.Fatalf("%s got invalid json: %v", label, err)
	}
	if err := json.Unmarshal(want, &wantObj); err != nil {
		t.Fatalf("%s want invalid json: %v", label, err)
	}
	gotNorm, _ := json.Marshal(gotObj)
	wantNorm, _ := json.Marshal(wantObj)
	if !bytes.Equal(gotNorm, wantNorm) {
		t.Fatalf("%s mismatch\nwant: %s\ngot: %s", label, string(wantNorm), string(gotNorm))
	}
}

func mustLoadCaseContract(t *testing.T, path string) fixture.CaseContract {
	t.Helper()
	contract, err := fixture.LoadCaseContract(path)
	if err != nil {
		t.Fatalf("load case contract %s: %v", path, err)
	}
	return contract
}

func assertCaseContract(t *testing.T, contract fixture.CaseContract) {
	t.Helper()
	assertAllowedFamily(t, "client.family", contract.Client.Family)
	assertAllowedFamily(t, "provider.family", contract.Provider.Family)
	assertAllowedDelivery(t, "client.delivery", contract.Client.Delivery)
	assertAllowedDelivery(t, "provider.delivery", contract.Provider.Delivery)
	if !contract.Assert.EnvelopeGrammarValid {
		t.Fatalf("case %s must enable assert.envelope_grammar_valid", contract.Name)
	}
	switch strings.TrimSpace(contract.FixtureSource) {
	case "synthetic", "captured":
	default:
		t.Fatalf("case %s fixture_source must be synthetic or captured", contract.Name)
	}
	if strings.TrimSpace(contract.CapturedAt) == "" {
		t.Fatalf("case %s must define captured_at", contract.Name)
	}
	if strings.TrimSpace(contract.FixtureSource) == "captured" && strings.TrimSpace(contract.CaptureRef) == "" {
		t.Fatalf("case %s capture_ref is required when fixture_source=captured", contract.Name)
	}
	if strings.TrimSpace(contract.FixtureSource) == "synthetic" && strings.TrimSpace(contract.CaptureRef) != "" {
		t.Fatalf("case %s capture_ref must be empty when fixture_source=synthetic", contract.Name)
	}
}

func validateExpectedCodeList(field string, values []string, caseName string) error {
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		code := strings.TrimSpace(raw)
		if code == "" {
			return fmt.Errorf("case %s %s must not contain empty code entry", caseName, field)
		}
		if _, ok := seen[code]; ok {
			return fmt.Errorf("case %s %s contains duplicate code %q", caseName, field, code)
		}
		seen[code] = struct{}{}
	}
	return nil
}

func assertAllowedFamily(t *testing.T, field string, value string) {
	t.Helper()
	switch strings.TrimSpace(value) {
	case "chatcompletions", "responses", "messages", "completions":
		return
	default:
		t.Fatalf("%s has unsupported value %q", field, value)
	}
}

func assertAllowedDelivery(t *testing.T, field string, value string) {
	t.Helper()
	switch strings.TrimSpace(value) {
	case "buffered", "streaming":
		return
	default:
		t.Fatalf("%s has unsupported value %q", field, value)
	}
}

func assertEnvelopeProjectionFromCanonicalEvents(t *testing.T, eventsPath string, clientBodyPath string) {
	t.Helper()
	events := readFixtureEvents(t, eventsPath)
	stream := canonical.NewSliceEventReader(events)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), stream, canonical.EnvResponse)
	if err != nil {
		t.Fatalf("project from events read closed envelope: %v", err)
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("project from events project response: %v", err)
	}
	if len(output.Items()) == 0 {
		t.Fatalf("projected output from %s has no items", eventsPath)
	}

	clientBody := readFile(t, clientBodyPath)
	for _, item := range output.Items() {
		if item.Kind == canonical.ItemKindText && strings.TrimSpace(item.Text) != "" {
			if !strings.Contains(clientBody, item.Text) {
				t.Fatalf("client body %s missing projected text %q", clientBodyPath, item.Text)
			}
		}
	}
}

func assertDeliveryConversionInvariants(t *testing.T, contract fixture.CaseContract, caseDir string) {
	t.Helper()
	providerSSE := readFile(t, filepath.Join(caseDir, "provider_response.sse"))
	clientSSE := readFile(t, filepath.Join(caseDir, "client_response.sse"))
	clientBody := readFile(t, filepath.Join(caseDir, "client_response.body.json"))

	if contract.Provider.Delivery == "streaming" {
		if !containsStreamTerminalMarker(providerSSE) {
			t.Fatalf("%s provider_response.sse must include terminal stream marker for streaming provider delivery", contract.Name)
		}
	}
	if contract.Client.Delivery == "streaming" {
		if !containsStreamTerminalMarker(clientSSE) {
			t.Fatalf("%s client_response.sse must include terminal stream marker for streaming client delivery", contract.Name)
		}
		assertStreamTerminalOrder(t, contract.Name, clientSSE)
	}
	if contract.Provider.Delivery == "streaming" && contract.Client.Delivery == "buffered" {
		if !strings.Contains(clientBody, "output_text") {
			t.Fatalf("%s buffered client projection must include output_text", contract.Name)
		}
	}
	if contract.Provider.Delivery == "buffered" && contract.Client.Delivery == "streaming" {
		if !strings.Contains(clientSSE, "response.output_text.delta") {
			t.Fatalf("%s synthesized stream must include response.output_text.delta", contract.Name)
		}
		if !strings.Contains(clientSSE, "response.completed") {
			if !strings.Contains(clientSSE, "[DONE]") {
				t.Fatalf("%s synthesized stream must include terminal completion marker", contract.Name)
			}
		}
	}
}

func containsStreamTerminalMarker(raw string) bool {
	return strings.Contains(raw, "response.completed") || strings.Contains(raw, "[DONE]")
}

func assertStreamTerminalOrder(t *testing.T, name string, raw string) {
	t.Helper()
	firstData := strings.Index(raw, "data:")
	firstTerminal := strings.Index(raw, "response.completed")
	if firstTerminal < 0 {
		firstTerminal = strings.Index(raw, "[DONE]")
	}
	if firstData >= 0 && firstTerminal >= 0 && firstTerminal < firstData {
		t.Fatalf("%s terminal marker appeared before first data frame", name)
	}
}

func readFixtureEvents(t *testing.T, path string) canonical.EventSequence {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	out := make(canonical.EventSequence, 0, 8)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var fe fixtureEvent
		if err := json.Unmarshal([]byte(line), &fe); err != nil {
			t.Fatalf("decode event line %q in %s: %v", line, path, err)
		}
		out = append(out, toCanonicalEvent(t, fe))
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s has no events", path)
	}
	return out
}

func toCanonicalEvent(t *testing.T, fe fixtureEvent) canonical.Event {
	t.Helper()
	ev := canonical.Event{
		ExchangeID: "fixture_exchange",
		Seq:        fe.Seq,
		Time:       time.Unix(0, fe.Seq),
		EnvID:      "res_1",
	}
	if strings.TrimSpace(fe.EnvID) != "" {
		ev.EnvID = canonical.EnvelopeID(strings.TrimSpace(fe.EnvID))
	}
	if strings.TrimSpace(fe.Parent) != "" {
		ev.ParentID = canonical.EnvelopeID(strings.TrimSpace(fe.Parent))
	}
	switch fe.Kind {
	case "envelope_start":
		ev.Kind = canonical.EventEnvelopeStart
		payload := canonical.EnvelopeStartPayload{Kind: canonical.EnvelopeKind(fe.EnvKind)}
		if payload.Kind == canonical.EnvMessage {
			payload.Role = canonical.ItemAuthorAssistant
		}
		ev.Payload = payload
	case "text_delta":
		ev.Kind = canonical.EventTextDelta
		ev.Payload = canonical.TextDeltaPayload{Text: fe.Text}
	case "envelope_end":
		ev.Kind = canonical.EventEnvelopeEnd
		ev.Payload = canonical.EnvelopeEndPayload{Kind: canonical.EnvelopeKind(fe.EnvKind), Status: canonical.EnvelopeStatus(fe.Status)}
	default:
		t.Fatalf("unsupported fixture event kind %q", fe.Kind)
	}
	return ev
}

func assertDirExists(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("missing directory %s: %v", path, err)
	}
	if !st.IsDir() {
		t.Fatalf("path is not directory: %s", path)
	}
}

func assertFileHasContent(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("missing file %s: %v", path, err)
	}
	if st.IsDir() {
		t.Fatalf("expected file but got directory: %s", path)
	}
	if st.Size() == 0 {
		t.Fatalf("file must not be empty: %s", path)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func repoRootFromHere(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}
