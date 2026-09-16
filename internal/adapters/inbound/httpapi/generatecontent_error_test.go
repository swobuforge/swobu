package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestMalformedGenerateContentOperationsUseGoogleEnvelope(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/c/personal/v1beta/models/x:generateContent"},
		{http.MethodPost, "/c/personal/v1beta/models/x:wrongAction"},
		{http.MethodPost, "/c/personal/v1beta/models/:generateContent"},
	} {
		response := httptest.NewRecorder()
		newTestHandler(nil).ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		var envelope struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("%s %s body=%q: %v", test.method, test.path, response.Body.String(), err)
		}
		if envelope.Error.Code == 0 || envelope.Error.Message == "" || envelope.Error.Status == "" {
			t.Fatalf("%s %s envelope=%#v", test.method, test.path, envelope)
		}
	}
}

func TestGenerateContentBackendErrorsUseGoogleEnvelope(t *testing.T) {
	backend := canonical.NewStructuredBackendError("provider", canonical.ClientFamilyResponses, 429, canonical.BackendErrorDetail{Message: "slow down", Type: "upstream"}, "")
	raw := projectBackendError(canonical.ClientFamilyGenerateContent, 429, backend)
	var body struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != 429 || body.Error.Message != "slow down" || body.Error.Status != "RESOURCE_EXHAUSTED" {
		t.Fatalf("body = %+v", body)
	}
	if json.Valid([]byte(backend.Message)) && string(raw) == backend.Message {
		t.Fatal("upstream envelope leaked")
	}
}
