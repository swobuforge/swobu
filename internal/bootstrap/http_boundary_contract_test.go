package bootstrap

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
)

func TestDaemonProtocolBoundaryDoesNotRedirectOrFallThroughToWeb(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(context.Background(), StartInput{
		ConfigPath:    filepath.Join(root, "swobu.yaml"),
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	cases := []struct {
		name        string
		method      string
		path        string
		contentType string
	}{
		{"post exact json", http.MethodPost, "/workspaces/default/v1/responses", "application/json"},
		{"post exact missing content type", http.MethodPost, "/workspaces/default/v1/responses", ""},
		{"post exact wrong content type", http.MethodPost, "/workspaces/default/v1/responses", "text/plain"},
		{"get exact json", http.MethodGet, "/workspaces/default/v1/responses", "application/json"},
		{"post trailing json", http.MethodPost, "/workspaces/default/v1/responses/", "application/json"},
		{"post trailing missing content type", http.MethodPost, "/workspaces/default/v1/responses/", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request, err := http.NewRequest(testCase.method, daemon.BaseURL()+testCase.path, strings.NewReader(`{"model":"default"}`))
			if err != nil {
				t.Fatal(err)
			}
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
				t.Fatalf("status=%d location=%q body=%q", response.StatusCode, response.Header.Get("Location"), body)
			}
			if strings.Contains(response.Header.Get("Content-Type"), "text/html") || strings.Contains(string(body), "<!doctype html>") {
				t.Fatalf("protocol request fell through to Web: status=%d content-type=%q body=%q", response.StatusCode, response.Header.Get("Content-Type"), body)
			}
		})
	}
}
