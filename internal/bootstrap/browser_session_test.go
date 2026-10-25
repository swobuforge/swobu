package bootstrap

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
)

func TestDaemonBrowserSessionLaunchAndControlAdmission(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	request := func(method, path, body string, browser bool) *http.Response {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if browser {
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("Origin", daemon.BaseURL())
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	check := func(response *http.Response, want int) {
		t.Helper()
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("status=%d want=%d body=%s", response.StatusCode, want, body)
		}
	}
	check(request("GET", "/api/v1/status", "", true), 401)
	check(request("GET", "/api/v1/connections", "", true), 401)
	invalidCookieRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet, daemon.BaseURL()+"/api/v1/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	invalidCookieRequest.AddCookie(&http.Cookie{Name: "swobu_operator_session", Value: strings.Repeat("x", 43)})
	invalidCookieResponse, err := client.Do(invalidCookieRequest)
	if err != nil {
		t.Fatal(err)
	}
	check(invalidCookieResponse, http.StatusUnauthorized)
	check(request("GET", "/workspaces", "", true), 401)
	launch := request("POST", "/api/v1/browser-launches", "", false)
	if launch.StatusCode != 201 {
		t.Fatal(launch.StatusCode)
	}
	var result struct {
		Path string `json:"launch_path"`
	}
	if err := json.NewDecoder(launch.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	launch.Body.Close()
	token := strings.TrimPrefix(result.Path, "/open#")
	check(request("POST", "/api/v1/browser-session", `{"token":"`+token+`"}`, true), 204)
	check(request("POST", "/api/v1/browser-session", `{"token":"`+token+`"}`, true), 401)
	check(request("GET", "/api/v1/status", "", true), 200)
	connections := request("GET", "/api/v1/connections", "", true)
	if connections.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("Connection response is cacheable")
	}
	check(connections, 200)
	check(request("GET", "/workspaces", "", true), 200)
	// Cookie possession never replaces the cross-origin boundary.
	r, _ := http.NewRequestWithContext(t.Context(), "GET", daemon.BaseURL()+"/api/v1/status", nil)
	r.Header.Set("Origin", "https://attacker.example")
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	check(response, 403)
}
