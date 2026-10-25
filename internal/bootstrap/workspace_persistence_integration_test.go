package bootstrap

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestDaemonMissingMCPConnectionCredentialDoesNotBlockOtherWorkspaceAndClosesSuccessfulBinding(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(config.EnvSwobuHome, stateRoot)
	t.Setenv(config.EnvAuthCredentialWritePolicy, "file")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publishMCPFixtureConnection(t, path, "cn_fixture_other")
	daemon, err := Start(context.Background(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	configClient := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	for _, slug := range []string{"research", "coding"} {
		intent, err := operatorclient.NewWorkspaceCreationIntent(workspaces.CreateWorkspace{Slug: slug}, "create-"+slug)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := configClient.ApplyWorkspaceCreation(t.Context(), intent); err != nil {
			t.Fatal(err)
		}
	}
	brokenID := publishBootstrapMCPConnection(t, daemon, "broken", "https://127.0.0.1:1/mcp", credentialref.Ref{})
	// Exercise an unavailable credential, not DNS for a fictional MCP server.
	t.Setenv("SWOBU_MISSING_RESTART_MCP_FIXTURE", "")
	missingID := publishBootstrapMCPConnection(t, daemon, "missing", "https://docs.example.test/mcp", credentialref.Parse("env:SWOBU_MISSING_RESTART_MCP_FIXTURE"))
	if response := mountMCPConnection(t, daemon, "research", "broken", brokenID, []string{"search"}); response.status != http.StatusOK {
		t.Fatalf("mount MCP source: status=%d body=%s", response.status, response.body)
	}
	if response := mountMCPConnection(t, daemon, "research", "missing", missingID, []string{"search"}); response.status != http.StatusOK {
		t.Fatalf("mount MCP source with missing secret: status=%d body=%s", response.status, response.body)
	}
	if response := requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/workspaces/research", ""); !strings.Contains(response.body, `"connection_id":"`+brokenID+`"`) {
		t.Fatalf("mount did not persist before restart: status=%d body=%s", response.status, response.body)
	}
	// The unrelated trusted source below is injected after startup; an operator
	// may not register the same source ID and accidentally replace that owner.
	// Its mount is installed through a prewired test-only runtime fixture below.
	setReviewRequiredMountForInjectedSource(t, daemon, "coding", "other", "cn_fixture_other", "search")

	broken, err := daemon.connectionStore.Get(mustConnectionID(t, brokenID))
	if err != nil {
		t.Fatal(err)
	}
	credential := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	ack, err := credential.ReplaceConnectionCredential(t.Context(), brokenID, broken.Revision().String(), "replace-broken", "test-upstream-secret", "")
	if err != nil || ack.Connection == nil {
		t.Fatalf("replace Connection credential err=%v", err)
	}
	node, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := node.ConnectionDocument()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(registryBytes), "test-upstream-secret") {
		t.Fatalf("durable MCP source registry contains credential material: %s", registryBytes)
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = daemon.Close()
	}()

	var closed []string
	toolKey, _ := canonical.NewToolKey("mcp/other", canonical.ToolKindFunction, "search")
	object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	tool, err := canonical.NewFunctionTool(toolKey, "Search docs", canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	if err != nil {
		t.Fatal(err)
	}
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "other")
	declaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{tool})
	if err != nil {
		t.Fatal(err)
	}
	source, _ := declaration.MCP()
	restarted, err := Start(context.Background(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}, MCPConnectionBindings: map[string]mcp.SourceBinding{
		"cn_fixture_other": {ID: "cn_fixture_other", ConnectionID: "cn_fixture_other", ConnectionRevision: "1", Source: source, NativeTools: map[string]json.RawMessage{"search": json.RawMessage(`{"name":"search","inputSchema":{"type":"object"}}`)}, Call: func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "docs"}}}, nil
		}, Close: func() error {
			closed = append(closed, "cn_fixture_other")
			return nil
		}},
	}})
	if err != nil {
		t.Fatalf("missing MCP source credential blocked daemon startup: %v", err)
	}
	defer restarted.Close()
	configAfter := restarted.configStore.Config()
	researchSlug, _ := routing.ParseWorkspaceSlug("research")
	codingSlug, _ := routing.ParseWorkspaceSlug("coding")
	research, researchExists := configAfter.Workspace(researchSlug)
	coding, codingExists := configAfter.Workspace(codingSlug)
	if !researchExists || !codingExists {
		t.Fatalf("unrelated workspaces lost after source failure: research=%v coding=%v", researchExists, codingExists)
	}
	if mounts := research.MCPMounts(); len(mounts) != 2 || mounts["broken"].ConnectionID != brokenID || mounts["missing"].ConnectionID != missingID {
		raw, _ := os.ReadFile(path)
		t.Fatalf("research MCP mounts=%+v persisted=%s", mounts, raw)
	}
	if _, err := mcp.WorkspaceCatalog(research, restarted.currentMCPBindings()); err == nil {
		t.Fatal("mount for unavailable source was published")
	}
	catalog, err := mcp.WorkspaceCatalog(coding, restarted.currentMCPBindings())
	if err != nil || len(catalog) != 1 || catalog[0].SourceID != "cn_fixture_other" {
		t.Fatalf("unrelated Workspace catalog=%v err=%v", catalog, err)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(closed, []string{"cn_fixture_other"}) {
		t.Fatalf("successful source sessions closed=%v", closed)
	}
}

// Keep the reserved socket open through child startup. Closing and rebinding
// races every other ephemeral-port user, including the child's TLS fixture.

type workspaceBearerTransport struct {
	next  http.RoundTripper
	token string
}

func (transport *workspaceBearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.Header = request.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+transport.token)
	return transport.next.RoundTrip(copy)
}

func writeBootstrapMCPCertificate(directory string) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mcp.example.test"},
		DNSNames: []string{"mcp.example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	privateKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	certificatePath := filepath.Join(directory, "mcp-root.pem")
	if err := os.WriteFile(certificatePath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return "", "", err
	}
	keyPath := filepath.Join(directory, "mcp-root-key.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateKey}), 0o600); err != nil {
		return "", "", err
	}
	return certificatePath, keyPath, nil
}

func TestDaemonRetiresMalformedMCPRegistryWithoutStartupCoupling(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "mcp-sources.json")
	if err := os.WriteFile(registryPath, []byte(`{"schema_version":1,"sources":{"docs":{"id":"docs","endpoint":"http://docs.example.test/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(context.Background(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil || daemon == nil {
		if daemon != nil {
			_ = daemon.Close()
		}
		t.Fatalf("retired MCP registry coupled to daemon startup daemon=%v err=%v", daemon, err)
	}
	defer daemon.Close()
}

func TestDaemonPersistsConnectionOwnedZAICredentialAcrossTargetReload(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvSwobuHome, root)
	t.Setenv(config.EnvAuthCredentialWritePolicy, "file")
	configPath := filepath.Join(root, "config", "swobu.yaml")
	input := StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}}
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { daemon.Close() }()
	idText := publishModelFixture(t, daemon, routing.ConnectionDraft{Provider: "zai", ZAI: &routing.ZAIConnectionDraft{Access: "coding_plan", Credential: "env:ZAI_INITIAL"}})
	id, err := connectionidentity.Parse(idText)
	if err != nil {
		t.Fatal(err)
	}
	original, err := daemon.connectionStore.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	slot := "connections/" + idText + "/cred_" + strings.Repeat("a", 32)
	ref := credentialref.Parse("secretfile:" + slot)
	_, err = daemon.connectionStore.StageCredential(t.Context(), id, []credentialref.Ref{ref}, func(context.Context) error { return nil }, func(context.Context) (credentialref.Ref, error) {
		raw, err := credentialsadapter.StoreConnectionCredentialSlot(idText, slot, "synthetic-zai-material", credentialsadapter.CredentialWritePolicyFile)
		return credentialref.Parse(raw), err
	})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := daemon.connectionStore.ReplaceCredentialBinding(t.Context(), id, original.Revision(), "", credentialref.Parse("env:ZAI_INITIAL"), ref, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf(`{"slug":"zai-persisted","initial_route":"chat","target":{"id":"primary","model":"manual-model","protocol":"chat_completions_stream","connection_id":%q}}`, idText)
	response := requestWorkspace(t, http.MethodPost, daemon.BaseURL()+"/api/v1/workspaces", command, "zai-persistence-creation")
	if response.status != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.status, response.body)
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("connection_id: "+idText)) || bytes.Contains(raw, []byte("credential:")) || bytes.Contains(raw, []byte("connection:")) {
		t.Fatal("routing did not retain reference-only target")
	}
	daemon, err = Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	response = requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/workspaces/zai-persisted", "")
	if response.status != http.StatusOK || !strings.Contains(response.body, `"connection_id":"`+idText+`"`) || !strings.Contains(response.body, `"protocol":"chat_completions_stream"`) || strings.Contains(response.body, ref.String()) {
		t.Fatalf("reloaded target status=%d body=%s", response.status, response.body)
	}
	current, err := daemon.connectionStore.Get(id)
	if err != nil || current.Revision() != committed.Revision() || !reflect.DeepEqual(current.CredentialRefs(), []credentialref.Ref{ref}) {
		t.Fatal("registry binding changed on reopen", err)
	}
	material, err := credentialsadapter.ResolveConnectionStoredBundle(idText, ref.String())
	bundle, recognized, decodeErr := credentialsadapter.DecodeTokenBundle(material)
	if err != nil || decodeErr != nil || !recognized || bundle.AccessToken != "synthetic-zai-material" {
		t.Fatal("Connection-owned synthetic material did not survive restart", err)
	}
}

func TestWorkspaceCommandPersistsAcrossDaemonRestart(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	initial := "schema_version: 5\nworkspaces: {}\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := Start(context.Background(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	connectionID := publishModelFixture(t, first, routing.ConnectionDraft{Provider: "openai", Standard: &routing.StandardConnectionDraft{Credential: "env:OPENAI_API_KEY"}})
	command := fmt.Sprintf(`{"slug":"persisted","initial_route":"chat","target":{"id":"primary","model":"gpt-5","protocol":"responses","connection_id":%q}}`, connectionID)
	response := requestWorkspace(t, http.MethodPost, first.BaseURL()+"/api/v1/workspaces", command, "restart-persistence-creation")
	if response.status != http.StatusCreated || !strings.Contains(response.body, `"slug":"persisted"`) {
		t.Fatalf("create status=%d body=%s", response.status, response.body)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"schema_version: 5", "persisted:", "id: primary", "protocol: responses"} {
		if !bytes.Contains(raw, []byte(token)) {
			t.Fatalf("persisted YAML missing %q:\n%s", token, raw)
		}
	}

	second, err := Start(context.Background(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	response = requestWorkspace(t, http.MethodGet, second.BaseURL()+"/api/v1/workspaces/persisted", "")
	if response.status != http.StatusOK || !strings.Contains(response.body, `"id":"primary"`) {
		t.Fatalf("restored status=%d body=%s", response.status, response.body)
	}
}

// Simulate an out-of-band routing removal without application revocation.
// Preserve historical retry results: textual name replacement can corrupt a
// receipt before reaching the live routing section of the same document.

func TestStartCreatesMissingRoutingConfigThroughStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "swobu.yaml")
	daemon, err := Start(context.Background(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "schema_version: 5\nworkspaces: {}\n" {
		t.Fatalf("initial document = %q", raw)
	}
}

type workspaceResponse struct {
	status int
	body   string
}

func requestWorkspace(t *testing.T, method, url, body string, retryKey ...string) workspaceResponse {
	t.Helper()
	if len(retryKey) > 1 {
		t.Fatal("fixture accepts at most one explicit retry key")
	}
	headers := map[string]string{}
	if len(retryKey) == 1 {
		headers["Idempotency-Key"] = retryKey[0]
	}
	return requestWorkspaceWithHeaders(t, method, url, body, headers)
}

func requestWorkspaceWithHeaders(t *testing.T, method, url, body string, headers map[string]string) workspaceResponse {

	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceResponse{status: response.StatusCode, body: string(raw)}
}

func setReviewRequiredMountForInjectedSource(t *testing.T, daemon *Daemon, workspace, alias, sourceID, tool string) {
	t.Helper()
	// Keep this lower-level fixture in routing setup so the proof can isolate an
	// unrelated Connection mount while another Connection fails at startup.
	config := daemon.configStore.Config()
	slug, _ := routing.ParseWorkspaceSlug(workspace)
	enabled, err := config.SetServiceEnabled(slug, routing.ServiceMCP, true)
	if err != nil {
		t.Fatal(err)
	}
	next, err := enabled.SetMCPMount(slug, alias, routing.MCPMount{ConnectionID: sourceID, AllowedTools: []string{tool}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.configStore.Update(t.Context(), func(routing.Config) (routing.Config, error) { return next, nil }); err != nil {
		t.Fatal(err)
	}
}

func publishBootstrapMCPConnection(t *testing.T, daemon *Daemon, name, rawEndpoint string, credential credentialref.Ref) string {
	t.Helper()
	id, err := connectionidentity.New()
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := connections.ParseEndpoint(rawEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewMCPHTTPSpec(endpoint, credential)
	if err != nil {
		t.Fatal(err)
	}
	spec, err = spec.WithOperatorDeclarations("fixture account", "fixture tool access")
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, name, spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.connectionStore.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func mustConnectionID(t *testing.T, raw string) connectionidentity.ID {
	t.Helper()
	id, err := connectionidentity.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mountMCPConnection(t *testing.T, daemon *Daemon, workspace, alias, connectionID string, tools []string) workspaceResponse {
	t.Helper()
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil {
		return workspaceResponse{status: http.StatusBadRequest, body: err.Error()}
	}
	if _, err := daemon.configStore.Update(t.Context(), func(current routing.Config) (routing.Config, error) {
		return current.SetServiceEnabled(slug, routing.ServiceMCP, true)
	}); err != nil {
		return workspaceResponse{status: http.StatusInternalServerError, body: err.Error()}
	}
	operator := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	preview, err := operator.PreviewMCPConnectionMount(t.Context(), workspace, alias, connectionID, tools)
	if err != nil {
		return workspaceResponse{status: http.StatusInternalServerError, body: err.Error()}
	}
	_, err = operator.ApplyMCPConnectionMount(t.Context(), workspace, alias, connectionID, tools, workspaces.MCPMountReviewRequest{WorkspaceETag: preview.Revision, Digest: preview.Digest, ConnectionRevision: preview.ConnectionRevision, AccessRevision: preview.AccessRevision, PublicationRevision: preview.PublicationRevision})
	if err != nil {
		return workspaceResponse{status: http.StatusInternalServerError, body: err.Error()}
	}
	return workspaceResponse{status: http.StatusOK, body: "ok"}
}
