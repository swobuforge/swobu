package bootstrap

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
)

// This is assembled local-daemon admission evidence. It proves the browser
// origin boundary around Node Control, not public data or browser release
// qualification journey.
func TestDaemonLocalControlRejectsBrowserOriginAndRebindAttempts(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{
		ConfigPath:    filepath.Join(root, "swobu.yaml"),
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	baseURL := daemon.BaseURL() + "/api/v1/status"
	baseRequest, err := http.NewRequest(http.MethodGet, baseURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	baseResponse, err := http.DefaultClient.Do(baseRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, baseResponse.Body)
	_ = baseResponse.Body.Close()
	if baseResponse.StatusCode != http.StatusOK {
		t.Fatalf("native local control status=%d, want %d", baseResponse.StatusCode, http.StatusOK)
	}

	port := baseRequest.URL.Port()
	cases := []struct {
		name   string
		host   string
		origin string
	}{
		{name: "malformed origin", host: "127.0.0.1:" + port, origin: "://"},
		{name: "foreign origin", host: "127.0.0.1:" + port, origin: "https://attacker.example"},
		{name: "dns rebind host", host: "attacker.example:" + port, origin: "http://attacker.example:" + port},
		{name: "origin path", host: "127.0.0.1:" + port, origin: "http://127.0.0.1:" + port + "/cross-site"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, baseURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Host = testCase.host
			request.Header.Set("Origin", testCase.origin)
			client := *http.DefaultClient
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("status=%d, want %d body=%s", response.StatusCode, http.StatusForbidden, body)
			}
			if response.Header.Get("Location") != "" {
				t.Fatal("local admission redirected an invalid browser request")
			}
		})
	}
}
