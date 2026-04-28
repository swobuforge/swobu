package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestDaemonProcessResponsesPreviousResponseIDTruthAndRecovery(t *testing.T) {
	errorFixture := mustReadResponsesFixture(t, "openai_previous_response_not_found.json")
	successFixture := mustReadResponsesFixture(t, "openrouter_buffered_ok.json")

	var requestBodies []string
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		requestBodies = append(requestBodies, string(raw))
		upstreamCalls++
		switch upstreamCalls {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(successFixture)
		case 2:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(errorFixture)
		case 3:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(successFixture)
		default:
			t.Fatalf("unexpected upstream call %d", upstreamCalls)
		}
	}))
	defer upstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				"alpha",
				"backend-a",
				mustProviderConfigWithModelID(
					t,
					harness.NewProviderConfig(
						t,
						"backend-a",
						"custom",
						upstream.URL+"/v1",
						"",
						protocolsurface.Responses,
					),
					"m",
				),
			),
		},
	})

	firstStatus, firstBody := postJSON(t, daemon.BaseURL+"/c/alpha/responses", `{"model":"m","input":"hi"}`)
	if firstStatus != http.StatusOK {
		t.Fatalf("first status = %d, want 200, body=%s", firstStatus, firstBody)
	}
	if !strings.Contains(strings.ToLower(firstBody), `"output_text":"ok"`) {
		t.Fatalf("first body = %q, want assistant output", firstBody)
	}
	firstResponseID := mustExtractResponseIDFromBody(t, firstBody)

	localMissStatus, localMissBody := postJSON(t, daemon.BaseURL+"/c/alpha/responses", `{"model":"m","previous_response_id":"resp_missing_local","input":"continue"}`)
	if localMissStatus != http.StatusBadRequest {
		t.Fatalf("local miss status = %d, want 400, body=%s", localMissStatus, localMissBody)
	}
	if !strings.Contains(localMissBody, `BAD_REQUEST`) || !strings.Contains(localMissBody, `previous_response_id could not be rehydrated`) {
		t.Fatalf("local miss body = %q, want explicit missing-parent failure", localMissBody)
	}

	recoveredStatus, recoveredBody := postJSON(t, daemon.BaseURL+"/c/alpha/responses", fmt.Sprintf(`{"model":"m","previous_response_id":"%s","input":"continue"}`, firstResponseID))
	if recoveredStatus != http.StatusOK {
		t.Fatalf("recovered status = %d, want 200, body=%s", recoveredStatus, recoveredBody)
	}
	if !strings.Contains(strings.ToLower(recoveredBody), `"output_text":"ok"`) {
		t.Fatalf("recovered body = %q, want assistant output", recoveredBody)
	}
	if strings.Contains(recoveredBody, `previous_response_not_found`) {
		t.Fatalf("recovered body = %q, want fallback success, not backend miss", recoveredBody)
	}

	if upstreamCalls != 3 {
		t.Fatalf("upstream calls = %d, want 3", upstreamCalls)
	}
	if got := requestBodies[0]; got != `{"model":"m","input":"hi"}` {
		t.Fatalf("first upstream body = %q, want %q", got, `{"model":"m","input":"hi"}`)
	}
	wantSecondBody := fmt.Sprintf(`{"model":"m","input":"continue","previous_response_id":"%s"}`, firstResponseID)
	if got := requestBodies[1]; got != wantSecondBody {
		t.Fatalf("native-parent body = %q, want delta input with previous_response_id", got)
	}
	if got := requestBodies[2]; got != `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},{"type":"message","role":"assistant","content":[{"type":"input_text","text":"OK"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}` {
		t.Fatalf("fallback body = %q, want canonical full-thread replay", got)
	}
}

func postJSON(t *testing.T, url string, body string) (int, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	return resp.StatusCode, string(raw)
}

func mustReadResponsesFixture(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("..", "fixtures", "responses_continuity", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	return raw
}

func mustExtractResponseIDFromBody(t *testing.T, body string) string {
	t.Helper()
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode response body id: %v", err)
	}
	if strings.TrimSpace(payload.ID) == "" {
		t.Fatal("response body missing id")
	}
	return payload.ID
}
