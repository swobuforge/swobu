package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	store "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/provider"
)

// The proxy keeps the production remote-MCP direct-dial guard in place: the
// fixture endpoint is a public-looking HTTPS authority, never a loopback URL.
// Its subprocess starts with proxy settings before Go initializes the cache.
var mcpAuditProxy struct {
	proxy       *httptest.Server
	certificate tls.Certificate
	keyFile     string
	certFile    string
	tempDir     string
	upstream    atomic.Pointer[httptest.Server]
}

func TestMain(m *testing.M) {
	if os.Getenv("SWOBU_HTTPAPI_MCP_AUDIT_CHILD") == "1" {
		var err error
		mcpAuditProxy.certificate, err = tls.LoadX509KeyPair(os.Getenv("SWOBU_HTTPAPI_MCP_AUDIT_CERT_FILE"), os.Getenv("SWOBU_HTTPAPI_MCP_AUDIT_KEY_FILE"))
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "load MCP Activity certificate fixture:", err)
			os.Exit(1)
		}
		// SSL_CERT_FILE is Unix-only. Confine the fixture root to this child
		// process so Windows also verifies the real TLS handshake without
		// installing a host certificate or bypassing hostname verification.
		certificate, err := x509.ParseCertificate(mcpAuditProxy.certificate.Certificate[0])
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "parse MCP Activity certificate fixture:", err)
			os.Exit(1)
		}
		roots := x509.NewCertPool()
		roots.AddCert(certificate)
		if err := os.Setenv("GODEBUG", os.Getenv("GODEBUG")+",x509usefallbackroots=1"); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "configure MCP Activity fixture roots:", err)
			os.Exit(1)
		}
		x509.SetFallbackRoots(roots)
		proxyListener, err := net.Listen("tcp", os.Getenv("SWOBU_HTTPAPI_MCP_AUDIT_PROXY_ADDR"))
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "allocate MCP Activity proxy fixture:", err)
			os.Exit(1)
		}
		if err := startMCPAuditProxy(proxyListener); err != nil {
			_ = proxyListener.Close()
			_, _ = fmt.Fprintln(os.Stderr, "initialize MCP Activity proxy fixture:", err)
			os.Exit(1)
		}
	}
	code := m.Run()
	if mcpAuditProxy.proxy != nil {
		mcpAuditProxy.proxy.Close()
	}
	if mcpAuditProxy.tempDir != "" {
		_ = os.RemoveAll(mcpAuditProxy.tempDir)
	}
	os.Exit(code)
}

func mcpAuditEndpoint(t *testing.T, handler http.Handler) string {
	t.Helper()
	upstream := httptest.NewUnstartedServer(handler)
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{mcpAuditProxy.certificate}}
	upstream.StartTLS()
	mcpAuditProxy.upstream.Store(upstream)
	t.Cleanup(upstream.Close)
	return "https://mcp.example.test:443/mcp"
}

func writeMCPAuditCertificate() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mcp.example.test"},
		DNSNames: []string{"mcp.example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}, &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mcp.example.test"},
		DNSNames: []string{"mcp.example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}, &key.PublicKey, key)
	if err != nil {
		return err
	}
	privateKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateKey}))
	if err != nil {
		return err
	}
	mcpAuditProxy.certificate = certificate
	mcpAuditProxy.tempDir, err = os.MkdirTemp("", "swobu-mcp-audit-")
	if err != nil {
		return err
	}
	certFile := filepath.Join(mcpAuditProxy.tempDir, "mcp-root.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return err
	}
	mcpAuditProxy.certFile = certFile
	keyFile := filepath.Join(mcpAuditProxy.tempDir, "mcp-root-key.pem")
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateKey}), 0600); err != nil {
		return err
	}
	mcpAuditProxy.keyFile = keyFile
	return nil
}

func startMCPAuditProxy(listener net.Listener) error {
	mcpAuditProxy.proxy = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "mcp.example.test:443" {
			http.Error(w, "unexpected CONNECT authority", http.StatusBadRequest)
			return
		}
		upstream := mcpAuditProxy.upstream.Load()
		if upstream == nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		conn, err := net.Dial("tcp", upstream.Listener.Addr().String())
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = conn.Close()
			return
		}
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { _, _ = io.Copy(conn, client); _ = conn.Close() }()
		_, _ = io.Copy(client, conn)
		_ = client.Close()
	}))
	mcpAuditProxy.proxy.Listener = listener
	mcpAuditProxy.proxy.Start()
	return nil
}

type mcpAuditTransport struct {
	calls        atomic.Int32
	firstRequest string
}

func (tr *mcpAuditTransport) Send(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
	call := tr.calls.Add(1)
	if call == 1 {
		tr.firstRequest = string(document.RawBytes())
	}
	if call == 1 {
		var wireRequest struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(document.RawBytes(), &wireRequest); err != nil {
			return nil, err
		}
		if len(wireRequest.Tools) != 1 {
			return nil, fmt.Errorf("provider attempt has %d tools, want discovered MCP search", len(wireRequest.Tools))
		}
		raw := `{"id":"provider-response","model":"model","output":[{"type":"function_call","call_id":"mcp_1","name":` + fmt.Sprintf("%q", wireRequest.Tools[0].Name) + `,"arguments":"{\"q\":\"evidence\"}"}]}`
		return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(raw), carrier.Meta{})}, nil
	}
	raw := `{"id":"provider-response-final","model":"model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`
	return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, []byte(raw), carrier.Meta{})}, nil
}

type mcpAuditRuntime struct {
	codecresolver.RuntimeCodecResolver
	transport *mcpAuditTransport
}

func (r mcpAuditRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return provider.Backend{Target: target, Codec: protocolcodec.Codec{Protocol: target.ProtocolKind}, Transport: provider.BindTransport(target, r.transport.Send)}, nil
}

func TestMCPCallTraversesExchangeEvidenceAndActivity(t *testing.T) {
	const childMarker = "SWOBU_HTTPAPI_MCP_AUDIT_CHILD"
	if os.Getenv(childMarker) != "1" {
		if err := writeMCPAuditCertificate(); err != nil {
			t.Fatal(err)
		}
		reserved, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		proxyAddress := reserved.Addr().String()
		if err := reserved.Close(); err != nil {
			t.Fatal(err)
		}
		command := exec.Command(os.Args[0], "-test.run=^TestMCPCallTraversesExchangeEvidenceAndActivity$", "-test.count=1")
		command.Env = make([]string, 0, len(os.Environ())+7)
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, childMarker+"=") &&
				!strings.HasPrefix(value, "SWOBU_HTTPAPI_MCP_AUDIT_PROXY_ADDR=") &&
				!strings.HasPrefix(value, "SWOBU_HTTPAPI_MCP_AUDIT_CERT_FILE=") &&
				!strings.HasPrefix(value, "SWOBU_HTTPAPI_MCP_AUDIT_KEY_FILE=") &&
				!strings.HasPrefix(value, "HTTPS_PROXY=") && !strings.HasPrefix(value, "https_proxy=") &&
				!strings.HasPrefix(value, "NO_PROXY=") && !strings.HasPrefix(value, "no_proxy=") {
				command.Env = append(command.Env, value)
			}
		}
		proxyURL := "http://" + proxyAddress
		command.Env = append(command.Env,
			childMarker+"=1",
			"SWOBU_HTTPAPI_MCP_AUDIT_PROXY_ADDR="+proxyAddress,
			"SWOBU_HTTPAPI_MCP_AUDIT_CERT_FILE="+mcpAuditProxy.certFile,
			"SWOBU_HTTPAPI_MCP_AUDIT_KEY_FILE="+mcpAuditProxy.keyFile,
			"HTTPS_PROXY="+proxyURL, "https_proxy="+proxyURL, "NO_PROXY=", "no_proxy=",
		)
		output, err := command.CombinedOutput()
		_ = os.RemoveAll(mcpAuditProxy.tempDir)
		if err != nil {
			t.Fatalf("isolated MCP Activity proof failed: %v\n%s", err, output)
		}
		return
	}

	var toolCalls atomic.Int32
	remote := sdk.NewServer(&sdk.Implementation{Name: "audit", Version: "1"}, nil)
	remote.AddTool(&sdk.Tool{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		toolCalls.Add(1)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "found"}}}, nil
	})
	// The fixture uses a public Host through a loopback CONNECT tunnel. The
	// SDK's localhost-Host check applies to this test server, not to Swobu's
	// outbound SSRF guard, which remains fully enabled.
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return remote }, &sdk.StreamableHTTPOptions{DisableLocalhostProtection: true})
	endpoint := mcpAuditEndpoint(t, handler)
	evidence := store.NewStore(store.Config{})
	transport := &mcpAuditTransport{}
	runtime := mcpAuditRuntime{RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(), transport: transport}
	ingress := exchange.NewIngress(imageIncidentWorkspaceLookup{workspace: cacheEvidenceWorkspace(t)}, runtime, exchange.RuntimePoliciesSpec{ModelConnection: lookupHTTPModel,
		PolicyResolver: exchange.StaticWorkspacePolicyResolver{Policy: exchange.DefaultWorkspacePolicy()}, ExecutionEvidence: evidence,
	})
	mux := http.NewServeMux()
	mux.Handle("/workspaces/", localAuthorityTestHandler(NewWorkspaceDataHandler(ingress)))
	mux.Handle("/api/v1/activity", NewActivityHandler(func(_ *http.Request, scope store.ProjectionScope, _ ledger.ActivityPageQuery) (store.ActivityProjection, error) {
		return evidence.ProjectActivity(store.ProjectionInput{Scope: scope}), nil
	}))
	server := httptest.NewServer(mux)
	defer server.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/workspaces/cache/responses", strings.NewReader(fmt.Sprintf(`{"model":"cache-route","input":"find evidence","stream":false,"tools":[{"type":"mcp","server_label":"docs","server_url":%q,"require_approval":"never"}]}`, endpoint)))
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("request status=%d body=%s provider request=%s", response.StatusCode, body, transport.firstRequest)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	if toolCalls.Load() != 1 || transport.calls.Load() != 2 {
		t.Fatalf("remote calls=%d provider calls=%d", toolCalls.Load(), transport.calls.Load())
	}
	lifecycles := evidence.Settled()
	if len(lifecycles) != 1 || lifecycles[0].Settled == nil || len(lifecycles[0].Invocations) != 3 {
		t.Fatalf("MCP exchange lifecycle = %#v", lifecycles)
	}
	var mcpCount int
	for i, invocation := range lifecycles[0].Invocations {
		if invocation.Started.Ordinal() != uint64(i+1) || invocation.Settled == nil {
			t.Fatalf("invocation %d = %#v", i, invocation)
		}
		if started, ok := invocation.Started.(executionevidence.MCPInvocationStarted); ok {
			mcpCount++
			settled, ok := invocation.Settled.(executionevidence.MCPInvocationSettled)
			if !ok || settled.Outcome() != execution.Succeeded || settled.Occurrence() != execution.Occurred || started.MCP().Tool().Name() != "search" {
				t.Fatalf("MCP lifecycle = %#v / %#v", started, invocation.Settled)
			}
		}
	}
	if mcpCount != 1 {
		t.Fatalf("MCP invocation count = %d", mcpCount)
	}
	activity, err := operatorclient.New(server.Client(), server.URL).Activity(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Exchanges) != 1 || activity.Exchanges[0].ExchangeID != lifecycles[0].Started.ID().String() || activity.Exchanges[0].State != "succeeded" || activity.Exchanges[0].AttemptCount != 2 || strings.TrimSpace(activity.Exchanges[0].Model) == "" {
		t.Fatalf("MCP exchange Activity = %#v", activity.Exchanges)
	}
}
