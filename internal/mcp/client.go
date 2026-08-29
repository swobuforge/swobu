package mcp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/platform/outboundhttp"
)

const (
	MaxListPages             = 32
	MaxToolsPerSource        = 256
	MaxHTTPResponseBytes     = 8 << 20
	MaxSchemaBytes           = 256 << 10
	MaxCatalogBytesPerSource = 4 << 20
	MaxToolResultBytes       = 1 << 20
	DiscoveryTimeout         = 15 * time.Second
	ToolCallTimeout          = 30 * time.Second
)

var errAuthenticationRequired = errors.New("MCP source requires authentication")

type session struct {
	source     canonical.MCPToolSource
	sdk        *mcp.ClientSession
	authStatus *authenticationStatus
}

func openSession(ctx context.Context, source canonical.MCPToolSource, access SourceAccess) (*session, error) {
	remote := source.Source()
	httpClient, authStatus, err := safeHTTPClient(source, access)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "swobu", Version: "v0"}, nil)
	endpoint, ok := remote.URL()
	if !ok {
		return nil, canonical.InternalError("local MCP client requires a URL source")
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint: endpoint, HTTPClient: httpClient,
		DisableStandaloneSSE: true, MaxRetries: -1,
	}
	sdkSession, err := client.Connect(ctx, transport, nil)
	if err != nil {
		if authStatus.denied.Load() {
			return nil, fmt.Errorf("%w: connect MCP source %q: %v", errAuthenticationRequired, source.Key().Name(), err)
		}
		return nil, fmt.Errorf("connect MCP source %q: %w", source.Key().Name(), err)
	}
	return &session{source: source.Clone(), sdk: sdkSession, authStatus: authStatus}, nil
}

func (s *session) listTools(ctx context.Context) ([]canonical.ToolDeclaration, []compat.Change, error) {
	ctx, cancel := context.WithTimeout(ctx, DiscoveryTimeout)
	defer cancel()
	var declarations []canonical.ToolDeclaration
	var changes []compat.Change
	discovered := make(map[string]canonical.ToolDeclaration)
	discoveredDecisions := make(map[string][]compat.Change)
	discoveredOrder := make([]string, 0)
	cursor := ""
	bounds := toolListBounds{
		bytes: len(s.source.Description()), seenCursors: map[string]struct{}{},
	}
	remote := s.source.Source()
	allowed, selected := remote.AllowedTools().Get()
	for {
		result, err := s.sdk.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			if s.authStatus != nil && s.authStatus.denied.Load() {
				return nil, nil, fmt.Errorf("%w: list MCP source %q tools: %v", errAuthenticationRequired, s.source.Key().Name(), err)
			}
			return nil, nil, fmt.Errorf("list MCP source %q tools: %w", s.source.Key().Name(), err)
		}
		if err := bounds.observe(len(result.Tools), result.NextCursor); err != nil {
			return nil, nil, fmt.Errorf("MCP source %q: %w", s.source.Key().Name(), err)
		}
		for _, tool := range result.Tools {
			if tool == nil {
				return nil, nil, fmt.Errorf("MCP source %q returned an empty tool", s.source.Key().Name())
			}
			retainedBytes, err := sdkToolRetainedBytes(s.source.Description(), tool)
			if err != nil {
				return nil, nil, fmt.Errorf("MCP tool %q catalog size: %w", tool.Name, err)
			}
			if err := bounds.observeBytes(retainedBytes); err != nil {
				return nil, nil, fmt.Errorf("MCP source %q: %w", s.source.Key().Name(), err)
			}
			declaration, toolDecisions, err := declarationFromSDKTool(s.source, tool)
			if err != nil {
				return nil, nil, err
			}
			if _, duplicate := discovered[tool.Name]; duplicate {
				return nil, nil, fmt.Errorf("MCP source %q returned duplicate tool %q", s.source.Key().Name(), tool.Name)
			}
			discovered[tool.Name] = declaration
			discoveredOrder = append(discoveredOrder, tool.Name)
			discoveredDecisions[tool.Name] = toolDecisions
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	if selected {
		for _, name := range allowed {
			declaration, ok := discovered[name]
			if !ok {
				return nil, nil, fmt.Errorf("MCP source %q did not declare selected tool %q", s.source.Key().Name(), name)
			}
			declarations = append(declarations, declaration)
			changes = append(changes, discoveredDecisions[name]...)
		}
	} else {
		for _, name := range discoveredOrder {
			declarations = append(declarations, discovered[name])
			changes = append(changes, discoveredDecisions[name]...)
		}
	}
	return declarations, changes, nil
}

func sdkToolRetainedBytes(sourceDescription string, tool *mcp.Tool) (int, error) {
	input, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return 0, err
	}
	var output []byte
	if tool.OutputSchema != nil {
		output, err = json.Marshal(tool.OutputSchema)
		if err != nil {
			return 0, err
		}
	}
	expandedDescription := tool.Description
	if sourceDescription != "" {
		expandedDescription = sourceDescription
		if tool.Description != "" {
			expandedDescription += "\n\n" + tool.Description
		}
	}
	return len(tool.Name) + len(tool.Description) + len(input) + len(output) +
		len(expandedDescription), nil
}

func declarationFromSDKTool(source canonical.MCPToolSource, tool *mcp.Tool) (canonical.ToolDeclaration, []compat.Change, error) {
	input, err := schemaFromSDK(tool.InputSchema)
	if err != nil {
		return canonical.ToolDeclaration{}, nil, fmt.Errorf("MCP tool %q input schema: %w", tool.Name, err)
	}
	key, err := canonical.NewToolKey(
		source.Key().Namespace()+"/"+source.Key().Name(),
		canonical.ToolKindFunction, tool.Name,
	)
	if err != nil {
		return canonical.ToolDeclaration{}, nil, err
	}
	declaration, err := canonical.NewFunctionTool(key, tool.Description, input, canonical.Unspecified[bool]())
	if err != nil {
		return canonical.ToolDeclaration{}, nil, err
	}
	var changes []compat.Change
	if tool.OutputSchema != nil {
		changes = append(changes, compat.Change{
			Capability: canonical.RequestTools,
			Kind:       compat.Approximation,
			Occurrence: canonical.ToolOccurrence(key),
		})
	}
	return declaration, changes, nil
}

type toolListBounds struct {
	pages       int
	tools       int
	bytes       int
	seenCursors map[string]struct{}
}

func (b *toolListBounds) observeBytes(count int) error {
	if count < 0 || b.bytes > MaxCatalogBytesPerSource-count {
		return fmt.Errorf("exceeded the catalog byte limit")
	}
	b.bytes += count
	return nil
}

func (b *toolListBounds) observe(toolCount int, nextCursor string) error {
	b.pages++
	if b.pages > MaxListPages {
		return fmt.Errorf("exceeded the tool-list page limit")
	}
	b.tools += toolCount
	if b.tools > MaxToolsPerSource {
		return fmt.Errorf("exceeded the tool limit")
	}
	if nextCursor != "" {
		if _, repeated := b.seenCursors[nextCursor]; repeated {
			return fmt.Errorf("repeated a tool-list cursor")
		}
		b.seenCursors[nextCursor] = struct{}{}
	}
	return nil
}

func (s *session) call(ctx context.Context, remoteName string, input canonical.JSONObject) ([]canonical.ToolResultPart, bool, error) {
	return s.callWithTimeout(ctx, remoteName, input, ToolCallTimeout)
}

func (s *session) callWithTimeout(ctx context.Context, remoteName string, input canonical.JSONObject, timeout time.Duration) ([]canonical.ToolResultPart, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var arguments map[string]any
	if err := json.Unmarshal([]byte(input.String()), &arguments); err != nil {
		return nil, false, err
	}
	result, err := s.sdk.CallTool(ctx, &mcp.CallToolParams{Name: remoteName, Arguments: arguments})
	if err != nil {
		return nil, false, fmt.Errorf("call MCP tool %q: %w", remoteName, err)
	}
	parts := make([]canonical.ToolResultPart, 0, len(result.Content)+1)
	totalBytes := 0
	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			return projectionFailureResult(), true, nil
		}
		totalBytes += len(text.Text)
		if totalBytes > MaxToolResultBytes {
			return nil, false, fmt.Errorf("MCP tool %q exceeded the result limit", remoteName)
		}
		parts = append(parts, canonical.NewTextToolResultPart(text.Text))
	}
	if result.StructuredContent != nil {
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return projectionFailureResult(), true, nil
		}
		totalBytes += len(raw)
		if totalBytes > MaxToolResultBytes {
			return projectionFailureResult(), true, nil
		}
		parts = append(parts, canonical.NewTextToolResultPart(string(raw)))
	}
	return parts, result.IsError, nil
}

func projectionFailureResult() []canonical.ToolResultPart {
	return []canonical.ToolResultPart{canonical.NewTextToolResultPart(
		"The MCP tool may have completed, but Swobu could not project its result content type.",
	)}
}

func (s *session) close() error {
	if s == nil || s.sdk == nil {
		return nil
	}
	return s.sdk.Close()
}

func schemaFromSDK(value any) (canonical.ToolSchema, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return canonical.ToolSchema{}, err
	}
	if len(raw) > MaxSchemaBytes {
		return canonical.ToolSchema{}, fmt.Errorf("schema exceeds %d bytes", MaxSchemaBytes)
	}
	object, err := canonical.ParseJSONObject(raw)
	if err != nil {
		return canonical.ToolSchema{}, err
	}
	return canonical.NewToolSchemaObject(object), nil
}

func safeHTTPClient(source canonical.MCPToolSource, access SourceAccess) (*http.Client, *authenticationStatus, error) {
	remote := source.Source()
	rawEndpoint, isURL := remote.URL()
	if !isURL {
		return nil, nil, canonical.InternalError("MCP client requires a URL source")
	}
	endpoint, err := url.Parse(rawEndpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, nil, canonical.BadRequest("MCP endpoint must be an absolute HTTPS URL without userinfo, query, or fragment")
	}
	host := endpoint.Hostname()
	port := endpoint.Port()
	if port == "" {
		port = "443"
	}
	// outboundhttp owns proxy route selection via Go's request-level
	// ProxyFromEnvironment. The restricted-direct guarded dialer runs only when
	// the per-request decision is "direct" — never on the first-hop address of an
	// already-selected proxy. The destination authority was validated at request
	// construction above (absolute HTTPS, no userinfo/query/fragment) and is
	// re-checked by the round-tripper chain below. SNI is supplied by the standard
	// transport from the request URL; only the TLS floor is pinned here.
	directDial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialHost, _, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(strings.TrimSuffix(dialHost, "."), strings.TrimSuffix(host, ".")) {
			return nil, fmt.Errorf("MCP dial destination changed origin")
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve MCP endpoint: %w", err)
		}
		if !publicMCPResolution(addresses) {
			return nil, fmt.Errorf("MCP endpoint resolution contains a prohibited address")
		}
		var last error
		for _, ip := range addresses {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = fmt.Errorf("MCP endpoint resolved no addresses")
		}
		return nil, last
	}
	transport := outboundhttp.NewTransport(outboundhttp.Config{
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		DirectDialContext: directDial,
	})
	var roundTripper http.RoundTripper = responseLimitTransport{next: transport}
	if len(access.Headers) > 0 {
		roundTripper = headerTransport{next: roundTripper, origin: endpointOrigin(endpoint), headers: access.Headers}
	}
	if access.Authorization != "" {
		roundTripper = bearerTransport{next: roundTripper, origin: endpointOrigin(endpoint), bearer: access.Authorization}
	}
	authStatus := &authenticationStatus{}
	roundTripper = authenticationStatusTransport{next: roundTripper, status: authStatus}
	return &http.Client{
		Transport:     roundTripper,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, authStatus, nil
}

type authenticationStatus struct{ denied atomic.Bool }

type authenticationStatusTransport struct {
	next   http.RoundTripper
	status *authenticationStatus
}

func (t authenticationStatusTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if response != nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
		t.status.denied.Store(true)
	}
	return response, err
}

type bearerTransport struct {
	next   http.RoundTripper
	origin string
	bearer string
}

type headerTransport struct {
	next    http.RoundTripper
	origin  string
	headers map[string]string
}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	if endpointOrigin(cloned.URL) == t.origin {
		for name, value := range t.headers {
			cloned.Header.Set(name, value)
		}
	}
	return t.next.RoundTrip(cloned)
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	if endpointOrigin(cloned.URL) == t.origin {
		cloned.Header.Set("Authorization", "Bearer "+t.bearer)
	}
	return t.next.RoundTrip(cloned)
}

func endpointOrigin(endpoint *url.URL) string {
	return strings.ToLower(endpoint.Scheme) + "://" + strings.ToLower(endpoint.Host)
}

type responseLimitTransport struct{ next http.RoundTripper }

func (t responseLimitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = &maxBytesReadCloser{source: response.Body, remaining: MaxHTTPResponseBytes}
	return response, nil
}

type maxBytesReadCloser struct {
	source    io.ReadCloser
	remaining int64
	exceeded  bool
}

func (r *maxBytesReadCloser) Read(buffer []byte) (int, error) {
	if r.exceeded {
		return 0, fmt.Errorf("MCP response exceeded %d bytes", MaxHTTPResponseBytes)
	}
	if int64(len(buffer)) > r.remaining+1 {
		buffer = buffer[:r.remaining+1]
	}
	n, err := r.source.Read(buffer)
	if int64(n) > r.remaining {
		r.exceeded = true
		return int(r.remaining), fmt.Errorf("MCP response exceeded %d bytes", MaxHTTPResponseBytes)
	}
	r.remaining -= int64(n)
	return n, err
}

func (r *maxBytesReadCloser) Close() error { return r.source.Close() }

func publicMCPAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, prefix := range prohibitedMCPPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func publicMCPResolution(addresses []netip.Addr) bool {
	if len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if !publicMCPAddress(address) {
			return false
		}
	}
	return true
}

var prohibitedMCPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("2001:db8::/32"),
}
