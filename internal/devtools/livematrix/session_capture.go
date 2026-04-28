package livematrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DirectionClientToSwobu   = "client->swobu"
	DirectionSwobuToClient   = "swobu->client"
	DirectionSwobuToProvider = "swobu->provider"
	DirectionProviderToSwobu = "provider->swobu"
)

var (
	buildSwobuOnce sync.Once
	builtSwobuPath string
	buildSwobuErr  error
)

func CaptureScenarioCaseViaSwobuTrace(ctx context.Context, httpClient *http.Client, scenarioCase ScenarioCase) (Capture, error) {
	startedAt := time.Now()
	capture := Capture{
		CapturedAt:   startedAt,
		ScenarioCase: scenarioCase,
		Session: &SessionTraceCapture{
			TraceID:   scenarioCase.ID + "-session",
			RequestID: scenarioCase.ID + "-request",
		},
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	resolved, err := ResolveScenarioCase(scenarioCase)
	if err != nil {
		capture.Error = err.Error()
		capture.DurationMS = int(time.Since(startedAt).Milliseconds())
		return capture, err
	}

	method, providerPath, payload, err := buildScenarioRequest(resolved)
	if err != nil {
		capture.Error = err.Error()
		capture.DurationMS = int(time.Since(startedAt).Milliseconds())
		return capture, err
	}
	requestBody, err := json.Marshal(payload)
	if err != nil {
		capture.Error = err.Error()
		capture.DurationMS = int(time.Since(startedAt).Milliseconds())
		return capture, err
	}

	recorder := &traceRecorder{
		trace: capture.Session,
	}
	relay, err := newProviderRelay(httpClient, resolved.BaseURL, recorder)
	if err != nil {
		capture.Error = err.Error()
		capture.DurationMS = int(time.Since(startedAt).Milliseconds())
		return capture, err
	}
	defer relay.Close()

	endpointName := traceCaseEndpointName(scenarioCase.ID)
	daemon, err := startDaemonForScenarioCase(ctx, resolved, endpointName, relay.URL)
	if err != nil {
		capture.Error = err.Error()
		capture.DurationMS = int(time.Since(startedAt).Milliseconds())
		return capture, err
	}
	defer func() {
		if closeErr := daemon.Close(); closeErr != nil && capture.Error == "" {
			capture.Error = closeErr.Error()
		}
	}()

	clientPath := "/c/" + endpointName + "/v1" + providerPath
	clientURL := daemon.baseURL + clientPath
	clientReq, err := http.NewRequestWithContext(ctx, method, clientURL, bytes.NewReader(requestBody))
	if err != nil {
		capture.Error = err.Error()
		capture.DurationMS = int(time.Since(startedAt).Milliseconds())
		return capture, err
	}
	clientReq.Header.Set("Content-Type", "application/json")
	clientReq.Header.Set("User-Agent", "swobu-live-matrix/1")
	if strings.EqualFold(resolved.Provider, "anthropic") && strings.EqualFold(resolved.Protocol, "messages") {
		clientReq.Header.Set("anthropic-version", "2023-06-01")
	}
	recorder.append(DirectionClientToSwobu, 0, CapturedWire{
		Method:  method,
		URL:     clientURL,
		Path:    clientPath,
		Headers: pickHeaders(clientReq.Header, "Content-Type", "User-Agent", "anthropic-version"),
		Body:    string(requestBody),
	})

	clientResp, err := httpClient.Do(clientReq)
	if err != nil {
		capture.Error = err.Error()
		capture.DurationMS = int(time.Since(startedAt).Milliseconds())
		return capture, err
	}
	defer func() { _ = clientResp.Body.Close() }()
	clientRespBody, _ := io.ReadAll(clientResp.Body)
	recorder.append(DirectionSwobuToClient, 0, CapturedWire{
		StatusCode: clientResp.StatusCode,
		Headers:    pickHeaders(clientResp.Header, "Content-Type", "X-Request-Id", "Retry-After"),
		Body:       string(clientRespBody),
	})

	capture.Client = CapturedFlow{
		Request: CapturedWire{
			Method:  method,
			URL:     clientURL,
			Path:    clientPath,
			Headers: pickHeaders(clientReq.Header, "Content-Type", "User-Agent", "anthropic-version"),
			Body:    string(requestBody),
		},
		Response: CapturedWire{
			StatusCode: clientResp.StatusCode,
			Headers:    pickHeaders(clientResp.Header, "Content-Type", "X-Request-Id", "Retry-After"),
			Body:       string(clientRespBody),
		},
	}

	egressReq, egressResp := recorder.firstAndLastEgress()
	capture.Request = egressReq
	capture.Response = egressResp
	if clientResp.StatusCode >= 400 {
		capture.Error = fmt.Sprintf("http %d", clientResp.StatusCode)
		capture.DurationMS = int(time.Since(startedAt).Milliseconds())
		return capture, fmt.Errorf("scenario_case %q failed with status %d", scenarioCase.ID, clientResp.StatusCode)
	}
	if capture.Response.StatusCode >= 400 {
		capture.Error = fmt.Sprintf("http %d", capture.Response.StatusCode)
		capture.DurationMS = int(time.Since(startedAt).Milliseconds())
		return capture, fmt.Errorf("scenario_case %q failed with status %d", scenarioCase.ID, capture.Response.StatusCode)
	}
	capture.DurationMS = int(time.Since(startedAt).Milliseconds())
	return capture, nil
}

type traceRecorder struct {
	trace *SessionTraceCapture
	mu    sync.Mutex
	seq   int
}

func (r *traceRecorder) append(direction string, attempt int, wire CapturedWire) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	r.trace.Events = append(r.trace.Events, TraceEvent{
		Seq:          r.seq,
		Direction:    direction,
		AttemptIndex: attempt,
		At:           time.Now(),
		Wire:         wire,
	})
}

func (r *traceRecorder) firstAndLastEgress() (CapturedWire, CapturedWire) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var req CapturedWire
	var resp CapturedWire
	for _, event := range r.trace.Events {
		switch event.Direction {
		case DirectionSwobuToProvider:
			if req.Method == "" {
				req = event.Wire
			}
		case DirectionProviderToSwobu:
			resp = event.Wire
		}
	}
	return req, resp
}

type providerRelayState struct {
	*httptest.Server
}

const relayUpstreamFailureStatusCode = 502

func newProviderRelay(client *http.Client, upstreamBaseURL string, recorder *traceRecorder) (*providerRelayState, error) {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	var attempts int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := int(atomic.AddInt64(&attempts, 1))
		inboundBody, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		recorder.append(DirectionSwobuToProvider, attempt, CapturedWire{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.Path,
			Headers: pickHeaders(r.Header, "Content-Type", "User-Agent", "anthropic-version"),
			Body:    string(inboundBody),
		})

		target := strings.TrimRight(upstreamBaseURL, "/") + r.URL.Path
		if strings.TrimSpace(r.URL.RawQuery) != "" {
			target += "?" + r.URL.RawQuery
		}
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(inboundBody))
		if err != nil {
			w.WriteHeader(relayUpstreamFailureStatusCode)
			_, _ = w.Write([]byte(`{"error":"relay request build failed"}`))
			recorder.append(DirectionProviderToSwobu, attempt, CapturedWire{
				StatusCode: relayUpstreamFailureStatusCode,
				Body:       `{"error":"relay request build failed"}`,
			})
			return
		}
		copyHeaders(outReq.Header, r.Header)
		// Preserve deterministic JSON payloads in captures by avoiding compressed upstream payloads.
		outReq.Header.Set("Accept-Encoding", "identity")
		resp, err := client.Do(outReq)
		if err != nil {
			w.WriteHeader(relayUpstreamFailureStatusCode)
			body := `{"error":"relay upstream call failed"}`
			_, _ = w.Write([]byte(body))
			recorder.append(DirectionProviderToSwobu, attempt, CapturedWire{
				StatusCode: relayUpstreamFailureStatusCode,
				Body:       body,
			})
			return
		}
		defer func() { _ = resp.Body.Close() }()
		responseBody, _ := io.ReadAll(resp.Body)
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(responseBody)

		recorder.append(DirectionProviderToSwobu, attempt, CapturedWire{
			StatusCode: resp.StatusCode,
			Headers:    pickHeaders(resp.Header, "Content-Type", "X-Request-Id", "Retry-After"),
			Body:       string(responseBody),
		})
	}))
	return &providerRelayState{Server: server}, nil
}

type daemonProcessCloser struct {
	baseURL    string
	daemonURL  string
	binaryPath string
	cmd        *exec.Cmd
	stdout     bytes.Buffer
	stderr     bytes.Buffer
}

func startDaemonForScenarioCase(ctx context.Context, scenarioCase ScenarioCase, endpointName string, relayBaseURL string) (*daemonProcessCloser, error) {
	binaryPath, err := swobuBinaryPath()
	if err != nil {
		return nil, err
	}
	configPath, err := renderRuntimeConfigForScenarioCase(scenarioCase, endpointName, relayBaseURL)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binaryPath, "daemon", "--config", configPath)
	proc := &daemonProcessCloser{
		binaryPath: binaryPath,
		cmd:        cmd,
	}
	cmd.Stdout = &proc.stdout
	cmd.Stderr = &proc.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start daemon: %w", err)
	}
	addr, err := proc.waitForBoundAddr(6 * time.Second)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	proc.baseURL = "http://" + addr
	proc.daemonURL = "http://" + addr
	if err := proc.waitForDaemonReady(6 * time.Second); err != nil {
		_ = proc.Close()
		return nil, err
	}
	return proc, nil
}

func (p *daemonProcessCloser) Close() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	down := exec.CommandContext(ctx, p.binaryPath, "down", "--daemon-url", p.daemonURL)
	_ = down.Run()
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		<-done
	case <-done:
	}
	return nil
}

func (p *daemonProcessCloser) waitForDaemonReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 800 * time.Millisecond}
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, p.daemonURL+"/_swobu/status", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return nil
			}
		}
		if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
			return fmt.Errorf("daemon exited early; stdout=%s stderr=%s", p.stdout.String(), p.stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon readiness timeout; stdout=%s stderr=%s", p.stdout.String(), p.stderr.String())
}

var (
	bindAddrKVPattern   = regexp.MustCompile(`\bbind_addr=([0-9.:]+)`)
	bindAddrJSONPattern = regexp.MustCompile(`"bind_addr":"([^"]+)"`)
)

func (p *daemonProcessCloser) waitForBoundAddr(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output := strings.TrimSpace(p.stdout.String() + "\n" + p.stderr.String())
		if output != "" {
			for _, match := range bindAddrKVPattern.FindAllStringSubmatch(output, -1) {
				if len(match) < 2 {
					continue
				}
				addr := strings.TrimSpace(match[1])
				if addr != "" && !strings.HasSuffix(addr, ":0") {
					return addr, nil
				}
			}
			for _, match := range bindAddrJSONPattern.FindAllStringSubmatch(output, -1) {
				if len(match) < 2 {
					continue
				}
				addr := strings.TrimSpace(match[1])
				if addr != "" && !strings.HasSuffix(addr, ":0") {
					return addr, nil
				}
			}
		}
		if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
			return "", fmt.Errorf("daemon exited early; stdout=%s stderr=%s", p.stdout.String(), p.stderr.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	return "", fmt.Errorf("daemon did not report bind address; stdout=%s stderr=%s", p.stdout.String(), p.stderr.String())
}

func swobuBinaryPath() (string, error) {
	buildSwobuOnce.Do(func() {
		tempDir, err := os.MkdirTemp("", "swobu-live-matrix-*")
		if err != nil {
			buildSwobuErr = err
			return
		}
		builtSwobuPath = filepath.Join(tempDir, "swobu-live-matrix-bin")
		cmd := exec.Command("go", "build", "-o", builtSwobuPath, "./cmd/swobu")
		cmd.Dir = repoRootFromWD()
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		output, runErr := cmd.CombinedOutput()
		if runErr != nil {
			buildSwobuErr = fmt.Errorf("%w: %s", runErr, strings.TrimSpace(string(output)))
			return
		}
	})
	if buildSwobuErr != nil {
		return "", fmt.Errorf("build swobu binary: %w", buildSwobuErr)
	}
	return builtSwobuPath, nil
}

func repoRootFromWD() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find repo root from working directory")
		}
		dir = parent
	}
}

func renderRuntimeConfigForScenarioCase(scenarioCase ScenarioCase, endpointName string, relayBaseURL string) (string, error) {
	credentialRef := ""
	if strings.TrimSpace(scenarioCase.APIKeyEnv) != "" {
		credentialRef = "env:" + strings.TrimSpace(scenarioCase.APIKeyEnv)
	}
	var b strings.Builder
	b.WriteString("bind_addr: 127.0.0.1:0\n")
	b.WriteString("endpoints:\n")
	b.WriteString("  - name: ")
	b.WriteString(endpointName)
	b.WriteString("\n")
	b.WriteString("    selected_provider_config_ref: backend-a\n")
	b.WriteString("    provider_configs:\n")
	b.WriteString("      - ref: backend-a\n")
	b.WriteString("        provider_spec: ")
	b.WriteString(strings.TrimSpace(scenarioCase.Provider))
	b.WriteString("\n")
	b.WriteString("        protocol_kind: ")
	b.WriteString(strings.TrimSpace(scenarioCase.Protocol))
	b.WriteString("\n")
	if strings.TrimSpace(scenarioCase.Model) != "" {
		b.WriteString("        model_id: ")
		b.WriteString(strings.TrimSpace(scenarioCase.Model))
		b.WriteString("\n")
	}
	b.WriteString("        base_url: ")
	b.WriteString(strings.TrimRight(relayBaseURL, "/"))
	b.WriteString("\n")
	if credentialRef != "" {
		b.WriteString("        credential_ref: ")
		b.WriteString(credentialRef)
		b.WriteString("\n")
	}

	path := filepath.Join(os.TempDir(), "swobu-live-matrix-"+endpointName+".yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func traceCaseEndpointName(raw string) string {
	candidate := strings.ToLower(strings.TrimSpace(raw))
	if candidate == "" {
		return "live"
	}
	var out strings.Builder
	for _, ch := range candidate {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			out.WriteRune(ch)
			continue
		}
		out.WriteRune('-')
	}
	value := strings.Trim(out.String(), "-")
	if value == "" {
		return "live"
	}
	if len(value) > 32 {
		value = value[:32]
	}
	return value
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		if strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
