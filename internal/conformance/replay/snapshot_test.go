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

	"github.com/swobuforge/swobu/internal/conformance/fixture"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
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
			assertExchangeReport(t, contract, filepath.Join(caseDir, "exchange_report.json"))
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
	var capturedProviderRequest []byte
	out, err := (exchange.Runner{}).Run(context.Background(), exchange.ClientInput{
		ExchangeID:       "fixture_exchange",
		ClientFamily:     clientFamily,
		ClientDelivery:   clientDelivery,
		ClientRequestRaw: clientRequest,
		ProviderFamily:   providerFamily,
		ProviderDelivery: providerDelivery,
		Target:           ports.NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", providerFamily, "", "", ""),
		Contract:         ports.NewExecutionContract(clientDelivery).WithProviderDelivery(providerDelivery),
		ProviderExecute: func(_ context.Context, req ports.ProviderRequest) (ports.ProviderTransportResponse, error) {
			wireReq, err := exchange.RealizeProviderRequestCarrier(req.Request, providerFamily, req.Contract.ProviderDelivery, "messages unsupported")
			if err != nil {
				return ports.ProviderTransportResponse{}, err
			}
			capturedProviderRequest = append(capturedProviderRequest[:0], wireReq.Raw...)
			if providerDelivery.Mode == delivery.Streaming {
				return ports.ProviderTransportResponse{
					Stream: io.NopCloser(strings.NewReader(providerResponseSSE)),
				}, nil
			}
			return ports.ProviderTransportResponse{
				Document: providerResponseBody,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("runner replay failed: %v", err)
	}
	assertJSONEqual(t, capturedProviderRequest, []byte(readFile(t, filepath.Join(caseDir, "provider_request.body.json"))), "provider request")
	if clientDelivery.Mode == delivery.Streaming {
		if out.Stream == nil {
			t.Fatalf("client stream missing")
		}
		streamRaw, readErr := io.ReadAll(out.Stream.Body)
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
	if !contract.Assert.NoUnreportedLoss {
		t.Fatalf("case %s must enable assert.no_unreported_loss", contract.Name)
	}
	if len(contract.Assert.ExpectedStageOrder) == 0 {
		t.Fatalf("case %s must define assert.expected_stage_order", contract.Name)
	}
	assertMutatedStagesSubsetOfExpectedOrder(t, contract)
	assertExpectedStageAppliedSubsetOfExpectedOrder(t, contract)
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
	if contract.Assert.MaxNotices < 0 {
		t.Fatalf("case %s max_notices must be >= 0", contract.Name)
	}
	if contract.Assert.MaxEvidence < 0 {
		t.Fatalf("case %s max_evidence must be >= 0", contract.Name)
	}
	if err := validateContractCodeMaxConsistency(contract); err != nil {
		t.Fatal(err)
	}
}

func validateContractCodeMaxConsistency(contract fixture.CaseContract) error {
	if len(contract.Assert.ExpectedNoticeCodes) > contract.Assert.MaxNotices {
		return fmt.Errorf("case %s expected_notice_codes count=%d exceeds max_notices=%d", contract.Name, len(contract.Assert.ExpectedNoticeCodes), contract.Assert.MaxNotices)
	}
	if len(contract.Assert.ExpectedEvidenceCodes) > contract.Assert.MaxEvidence {
		return fmt.Errorf("case %s expected_evidence_codes count=%d exceeds max_evidence=%d", contract.Name, len(contract.Assert.ExpectedEvidenceCodes), contract.Assert.MaxEvidence)
	}
	if err := validateExpectedCodeList("expected_notice_codes", contract.Assert.ExpectedNoticeCodes, contract.Name); err != nil {
		return err
	}
	if err := validateExpectedCodeList("expected_evidence_codes", contract.Assert.ExpectedEvidenceCodes, contract.Name); err != nil {
		return err
	}
	return nil
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

func assertExchangeReport(t *testing.T, contract fixture.CaseContract, path string) {
	t.Helper()
	report := readExchangeReport(t, path)
	if strings.TrimSpace(report.ExchangeID) == "" {
		t.Fatalf("exchange report %s missing exchange_id", path)
	}
	if len(report.Stages) == 0 {
		t.Fatalf("exchange report %s missing stages", path)
	}
	if err := validateLossPolicy(contract.Assert.NoLossAllowed, contract.Assert.NoUnreportedLoss, report.Losses, path); err != nil {
		t.Fatal(err)
	}
	assertStageOrder(t, report.Stages, contract.Assert.ExpectedStageOrder, path)
	assertMutatedStages(t, report.Stages, contract.Assert.ExpectedMutatedStages, path)
	assertStageShape(t, report.Stages, path)
	assertStageApplied(t, report.Stages, contract.Assert.ExpectedStageApplied, path)
	if err := validateMaxCount("notices", len(report.Notices), contract.Assert.MaxNotices, path); err != nil {
		t.Fatal(err)
	}
	if err := validateMaxCount("evidence", len(report.Evidence), contract.Assert.MaxEvidence, path); err != nil {
		t.Fatal(err)
	}
	assertCodeSet(t, "notice", noticeCodes(report.Notices), contract.Assert.ExpectedNoticeCodes, path)
	assertCodeSet(t, "evidence", evidenceCodes(report.Evidence), contract.Assert.ExpectedEvidenceCodes, path)
}

func validateLossPolicy(noLossAllowed bool, noUnreportedLoss bool, losses []exchange.ProjectionLoss, path string) error {
	if noLossAllowed && len(losses) > 0 {
		return fmt.Errorf("exchange report %s has losses but case requires no_loss_allowed", path)
	}
	if noUnreportedLoss {
		for _, loss := range losses {
			if strings.TrimSpace(loss.Field) == "" || strings.TrimSpace(loss.Reason) == "" {
				return fmt.Errorf("exchange report %s contains unreported loss entry: %+v", path, loss)
			}
		}
	}
	return nil
}

func validateMaxCount(label string, actual int, max int, path string) error {
	if actual > max {
		return fmt.Errorf("exchange report %s %s=%d exceeds max_%s=%d", path, label, actual, label, max)
	}
	return nil
}

func assertStageOrder(t *testing.T, got []exchange.StageReport, expected []string, path string) {
	t.Helper()
	if err := validateStageOrder(got, expected, path); err != nil {
		t.Fatal(err)
	}
}

func validateStageOrder(got []exchange.StageReport, expected []string, path string) error {
	idx := 0
	for _, stage := range got {
		if idx < len(expected) && string(stage.Stage) == expected[idx] {
			idx++
		}
	}
	if idx != len(expected) {
		return fmt.Errorf("exchange report %s stage order missing expected sequence %#v; got %#v", path, expected, got)
	}
	return nil
}

func assertMutatedStages(t *testing.T, got []exchange.StageReport, expected []string, path string) {
	t.Helper()
	if err := validateMutatedStages(got, expected, path); err != nil {
		t.Fatal(err)
	}
}

func assertMutatedStagesSubsetOfExpectedOrder(t *testing.T, contract fixture.CaseContract) {
	t.Helper()
	if err := validateMutatedStagesSubsetOfExpectedOrder(contract); err != nil {
		t.Fatal(err)
	}
}

func validateMutatedStages(got []exchange.StageReport, expected []string, path string) error {
	expectedSet := make(map[string]struct{}, len(expected))
	for _, stage := range expected {
		expectedSet[strings.TrimSpace(stage)] = struct{}{}
	}
	actualSet := make(map[string]struct{})
	for _, stage := range got {
		if stage.Mutated {
			name := strings.TrimSpace(string(stage.Stage))
			actualSet[name] = struct{}{}
			if _, ok := expectedSet[name]; !ok {
				return fmt.Errorf("exchange report %s has unexpected mutated stage %q; expected mutated stages=%v", path, name, mapKeys(expectedSet))
			}
		}
	}
	if len(actualSet) != len(expectedSet) {
		return fmt.Errorf("exchange report %s mutated stages mismatch: got=%v expected=%v", path, mapKeys(actualSet), mapKeys(expectedSet))
	}
	for k := range expectedSet {
		if _, ok := actualSet[k]; !ok {
			return fmt.Errorf("exchange report %s missing expected mutated stage %q; got=%v", path, k, mapKeys(actualSet))
		}
	}
	return nil
}

func validateMutatedStagesSubsetOfExpectedOrder(contract fixture.CaseContract) error {
	orderSet := make(map[string]struct{}, len(contract.Assert.ExpectedStageOrder))
	for _, stage := range contract.Assert.ExpectedStageOrder {
		orderSet[strings.TrimSpace(stage)] = struct{}{}
	}
	for _, stage := range contract.Assert.ExpectedMutatedStages {
		name := strings.TrimSpace(stage)
		if _, ok := orderSet[name]; !ok {
			return fmt.Errorf("case %s has expected_mutated_stages entry %q not present in expected_stage_order", contract.Name, name)
		}
	}
	return nil
}

func assertExpectedStageAppliedSubsetOfExpectedOrder(t *testing.T, contract fixture.CaseContract) {
	t.Helper()
	if err := validateExpectedStageAppliedSubsetOfExpectedOrder(contract); err != nil {
		t.Fatal(err)
	}
}

func validateExpectedStageAppliedSubsetOfExpectedOrder(contract fixture.CaseContract) error {
	orderSet := make(map[string]struct{}, len(contract.Assert.ExpectedStageOrder))
	for _, stage := range contract.Assert.ExpectedStageOrder {
		orderSet[strings.TrimSpace(stage)] = struct{}{}
	}
	for stage, applied := range contract.Assert.ExpectedStageApplied {
		name := strings.TrimSpace(stage)
		if _, ok := orderSet[name]; !ok {
			return fmt.Errorf("case %s has expected_stage_applied key %q not present in expected_stage_order", contract.Name, name)
		}
		seen := map[string]struct{}{}
		for _, rawID := range applied {
			id := strings.TrimSpace(rawID)
			if id == "" {
				return fmt.Errorf("case %s expected_stage_applied[%s] contains empty transform id", contract.Name, name)
			}
			if _, ok := seen[id]; ok {
				return fmt.Errorf("case %s expected_stage_applied[%s] contains duplicate transform id %q", contract.Name, name, id)
			}
			seen[id] = struct{}{}
		}
	}
	return nil
}

func assertStageShape(t *testing.T, got []exchange.StageReport, path string) {
	t.Helper()
	for _, stage := range got {
		name := strings.TrimSpace(string(stage.Stage))
		if name == "" {
			t.Fatalf("exchange report %s contains empty stage id", path)
		}
		switch stage.Stage {
		case string(exchange.StageClientHTTPIn),
			string(exchange.StageClientWireIn),
			string(exchange.StageSemanticRequest),
			string(exchange.StageProviderWireOut),
			string(exchange.StageProviderHTTPOut),
			string(exchange.StageProviderHTTPIn),
			string(exchange.StageProviderWireIn),
			string(exchange.StageSemanticEvents),
			string(exchange.StageClientWireOut),
			string(exchange.StageClientHTTPOut):
		default:
			t.Fatalf("exchange report %s stage=%q is not a known exchange stage id", path, name)
		}
		if stage.Stage == string(exchange.StageProviderWireOut) || stage.Stage == string(exchange.StageProviderWireIn) || stage.Stage == string(exchange.StageSemanticEvents) {
			if strings.TrimSpace(stage.Carrier) == "" {
				t.Fatalf("exchange report %s stage=%q must include carrier", path, name)
			}
		}
		if stage.Mutated && len(stage.Applied) == 0 {
			t.Fatalf("exchange report %s stage=%q mutated=true requires non-empty applied transform ids", path, name)
		}
	}
}

func assertStageApplied(t *testing.T, got []exchange.StageReport, expected map[string][]string, path string) {
	t.Helper()
	if err := validateStageApplied(got, expected, path); err != nil {
		t.Fatal(err)
	}
}

func validateStageApplied(got []exchange.StageReport, expected map[string][]string, path string) error {
	if len(expected) == 0 {
		return nil
	}
	byStage := map[string]exchange.StageReport{}
	for _, stage := range got {
		byStage[strings.TrimSpace(stage.Stage)] = stage
	}
	for stageNameRaw, expectedAppliedRaw := range expected {
		stageName := strings.TrimSpace(stageNameRaw)
		report, ok := byStage[stageName]
		if !ok {
			return fmt.Errorf("exchange report %s missing stage %q required by expected_stage_applied", path, stageName)
		}
		expectedApplied := append([]string(nil), expectedAppliedRaw...)
		for i := range expectedApplied {
			expectedApplied[i] = strings.TrimSpace(expectedApplied[i])
		}
		actualApplied := append([]string(nil), report.Applied...)
		for i := range actualApplied {
			actualApplied[i] = strings.TrimSpace(actualApplied[i])
		}
		if len(actualApplied) != len(expectedApplied) {
			return fmt.Errorf("exchange report %s stage %q applied mismatch: got=%v expected=%v", path, stageName, actualApplied, expectedApplied)
		}
		for idx := range expectedApplied {
			if actualApplied[idx] != expectedApplied[idx] {
				return fmt.Errorf("exchange report %s stage %q applied mismatch: got=%v expected=%v", path, stageName, actualApplied, expectedApplied)
			}
		}
	}
	return nil
}

func mapKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func assertCodeSet(t *testing.T, label string, got []string, expected []string, path string) {
	t.Helper()
	if err := validateCodeSet(label, got, expected, path); err != nil {
		t.Fatal(err)
	}
}

func validateCodeSet(label string, got []string, expected []string, path string) error {
	expectedSet := make(map[string]struct{}, len(expected))
	for _, code := range expected {
		expectedSet[strings.TrimSpace(code)] = struct{}{}
	}
	actualSet := make(map[string]struct{}, len(got))
	for _, codeRaw := range got {
		if strings.TrimSpace(codeRaw) == "" {
			continue
		}
		actualSet[strings.TrimSpace(codeRaw)] = struct{}{}
	}
	if len(actualSet) != len(expectedSet) {
		return fmt.Errorf("exchange report %s %s codes mismatch: got=%v expected=%v", path, label, mapKeys(actualSet), mapKeys(expectedSet))
	}
	for code := range expectedSet {
		if _, ok := actualSet[code]; !ok {
			return fmt.Errorf("exchange report %s missing expected %s code %q; got=%v", path, label, code, mapKeys(actualSet))
		}
	}
	return nil
}

func readExchangeReport(t *testing.T, path string) exchange.ExchangeReport {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out exchange.ExchangeReport
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode exchange report %s: %v", path, err)
	}
	return out
}

func noticeCodes(in []exchange.Notice) []string {
	out := make([]string, 0, len(in))
	for _, n := range in {
		out = append(out, n.Code)
	}
	return out
}

func evidenceCodes(in []exchange.Evidence) []string {
	out := make([]string, 0, len(in))
	for _, e := range in {
		out = append(out, e.Code)
	}
	return out
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
