package mcp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const (
	MaxSourcesPerRequest       = 8
	MaxRemoteCallsPerRun       = 16
	MaxRemoteRoundsPerRun      = 8
	MaxCatalogBytesPerRun      = 16 << 20
	MaxDependencyEvidenceBytes = 64 << 10
)

type binding struct {
	source     canonical.ToolKey
	remoteName string
}

type sourceResolution struct {
	session      *session
	catalog      canonical.ToolNamespace
	catalogBytes int
	changes      []compat.Change
}

type sourceResolver func(context.Context, canonical.ToolNamespace, SourceAccess) (sourceResolution, error)

// Run is one exchange's fully opened MCP aggregate. It privately owns source
// availability, SDK sessions, execution bindings, the attempt transformation, and
// remote-effect budgets.
type Run struct {
	sessions         map[canonical.ToolKey]*session
	bindings         map[canonical.ToolKey]binding
	attemptTools     map[canonical.ToolKey][]canonical.ToolDeclaration
	localSources     map[canonical.ToolKey]struct{}
	callCount        int
	roundCount       int
	firstUnavailable error
}

// Open resolves every MCP source independently and returns exact canonical
// history plus one request-scoped runtime. Optional source failures affect only
// the runtime's attempt view; required source failures remain terminal.
func Open(
	ctx context.Context,
	request canonical.CanonicalRequest,
	access Access,
) (canonical.CanonicalRequest, *Run, []compat.Change, error) {
	return openWith(ctx, request, access, resolveRemoteSource)
}

func openWith(
	ctx context.Context,
	request canonical.CanonicalRequest,
	access Access,
	resolve sourceResolver,
) (canonical.CanonicalRequest, *Run, []compat.Change, error) {
	if resolve == nil {
		return canonical.CanonicalRequest{}, nil, nil, canonical.InternalError("MCP source resolver is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, DiscoveryTimeout)
	defer cancel()
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return canonical.CanonicalRequest{}, nil, nil, err
	}
	sources := localMCPSources(environment)
	if len(sources) == 0 {
		return request.Clone(), nil, nil, nil
	}
	policy, err := request.EffectiveToolPolicy()
	if err != nil {
		return canonical.CanonicalRequest{}, nil, nil, err
	}
	activeSources := 0
	for _, source := range sources {
		remote, _ := source.MCPSource()
		allowed, selected := remote.AllowedTools().Get()
		if policy.Mode != canonical.ToolPolicyNone && (!selected || len(allowed) > 0) {
			activeSources++
		}
	}
	if activeSources > MaxSourcesPerRequest {
		return canonical.CanonicalRequest{}, nil, nil, canonical.BadRequest("responses request exceeds the remote MCP source limit")
	}
	sources = requiredSourcesFirst(request, environment, sources)

	run := &Run{
		sessions:     make(map[canonical.ToolKey]*session),
		bindings:     make(map[canonical.ToolKey]binding),
		attemptTools: make(map[canonical.ToolKey][]canonical.ToolDeclaration),
		localSources: make(map[canonical.ToolKey]struct{}, len(sources)),
	}
	catalogs := make(map[canonical.ToolKey]canonical.ToolNamespace, len(sources))
	var changes []compat.Change
	catalogBytes := 0
	for _, source := range sources {
		key := source.Key()
		run.localSources[key] = struct{}{}
		remote, _ := source.MCPSource()
		allowed, selected := remote.AllowedTools().Get()
		if policy.Mode == canonical.ToolPolicyNone || (selected && len(allowed) == 0) {
			continue
		}
		resolved, resolveErr := resolve(ctx, source, access.ForSource(key))
		if resolveErr != nil {
			if sourceFailureIsInvariant(resolveErr) || requestRequiresSource(request, environment, source) {
				_ = run.Close()
				return canonical.CanonicalRequest{}, nil, changes, resolveErr
			}
			if run.firstUnavailable == nil {
				run.firstUnavailable = resolveErr
			}
			changes = append(changes, droppedSourceDecision(key))
			continue
		}
		if resolved.catalogBytes == 0 {
			resolved.catalogBytes = retainedCatalogBytes(resolved.catalog)
		}
		if resolved.catalogBytes > MaxCatalogBytesPerSource ||
			catalogBytes > MaxCatalogBytesPerRun-resolved.catalogBytes {
			_ = resolved.session.close()
			dependency := dependencyError(
				source, errors.New("MCP catalog exceeds the exchange byte limit"),
			)
			if requestRequiresSource(request, environment, source) {
				_ = run.Close()
				return canonical.CanonicalRequest{}, nil, changes, dependency
			}
			if run.firstUnavailable == nil {
				run.firstUnavailable = dependency
			}
			changes = append(changes, droppedSourceDecision(key))
			continue
		}
		catalogBytes += resolved.catalogBytes
		run.sessions[key] = resolved.session
		catalogs[key] = resolved.catalog
		sourceBindings, attemptTools, bindingDecisions, bindingErr := bindingsForCatalog(resolved.catalog)
		if bindingErr != nil {
			_ = resolved.session.close()
			delete(run.sessions, key)
			dependency := dependencyError(source, bindingErr)
			if requestRequiresSource(request, environment, source) {
				_ = run.Close()
				return canonical.CanonicalRequest{}, nil, changes, dependency
			}
			if run.firstUnavailable == nil {
				run.firstUnavailable = dependency
			}
			changes = append(changes, droppedSourceDecision(key))
			delete(catalogs, key)
			continue
		}
		for tool, toolBinding := range sourceBindings {
			run.bindings[tool] = toolBinding
		}
		run.attemptTools[key] = attemptTools
		changes = append(changes, resolved.changes...)
		changes = append(changes, bindingDecisions...)
		if remote.Loading() == canonical.MCPLoadingDeferred {
			changes = append(changes, compat.Change{
				Capability: canonical.RequestTools,
				Kind:       compat.Approximation,
				Occurrence: canonical.ToolOccurrence(key),
				Preserved:  canonical.RequestTools,
			})
		}
	}

	prepared, err := applyCatalogs(request, catalogs)
	if err != nil {
		_ = run.Close()
		return canonical.CanonicalRequest{}, nil, changes, err
	}
	attempt, err := run.rewriteAttempt(prepared)
	if err != nil {
		_ = run.Close()
		return canonical.CanonicalRequest{}, nil, changes, err
	}
	if err := run.validateAttemptPolicy(attempt); err != nil {
		_ = run.Close()
		return canonical.CanonicalRequest{}, nil, changes, err
	}
	return prepared, run, changes, nil
}

func resolveRemoteSource(
	ctx context.Context,
	source canonical.ToolNamespace,
	access SourceAccess,
) (sourceResolution, error) {
	opened, err := openSession(ctx, source, access)
	if err != nil {
		return sourceResolution{}, sourceError(source, err)
	}
	children := source.Tools()
	var changes []compat.Change
	if len(children) == 0 {
		children, changes, err = opened.listTools(ctx)
		if err != nil {
			_ = opened.close()
			return sourceResolution{}, dependencyError(source, err)
		}
	}
	declaration, err := canonical.NewMCPToolNamespace(
		source.Key(), source.Description(), mustMCPSource(source), children,
	)
	if err != nil {
		_ = opened.close()
		return sourceResolution{}, dependencyError(source, err)
	}
	catalog, _ := declaration.Namespace()
	return sourceResolution{
		session: opened, catalog: catalog,
		catalogBytes: retainedCatalogBytes(catalog),
		changes:      changes,
	}, nil
}

func localMCPSources(environment canonical.ToolEnvironment) []canonical.ToolNamespace {
	var sources []canonical.ToolNamespace
	seen := make(map[canonical.ToolKey]struct{})
	for _, declaration := range environment.Declarations() {
		namespace, ok := declaration.Namespace()
		if !ok {
			continue
		}
		source, ok := namespace.MCPSource()
		if !ok || source.Kind() != canonical.MCPSourceURL ||
			source.Approval().Kind() != canonical.MCPApprovalNever {
			continue
		}
		if _, restricted := source.AllowedCallers().Get(); restricted {
			continue
		}
		if _, duplicate := seen[namespace.Key()]; duplicate {
			continue
		}
		seen[namespace.Key()] = struct{}{}
		sources = append(sources, namespace)
	}
	return sources
}

func requiredSourcesFirst(
	request canonical.CanonicalRequest,
	environment canonical.ToolEnvironment,
	sources []canonical.ToolNamespace,
) []canonical.ToolNamespace {
	ordered := make([]canonical.ToolNamespace, 0, len(sources))
	for _, required := range []bool{true, false} {
		for _, source := range sources {
			if requestRequiresSource(request, environment, source) == required {
				ordered = append(ordered, source)
			}
		}
	}
	return ordered
}

func bindingsForCatalog(catalog canonical.ToolNamespace) (
	map[canonical.ToolKey]binding,
	[]canonical.ToolDeclaration,
	[]compat.Change,
	error,
) {
	bindings := make(map[canonical.ToolKey]binding)
	catalogTools := catalog.Tools()
	attemptTools := make([]canonical.ToolDeclaration, 0, len(catalogTools))
	for _, declaration := range catalogTools {
		function, ok := declaration.Function()
		if !ok {
			return nil, nil, nil, errors.New("MCP catalog contains a non-function child")
		}
		description := function.Description()
		if sourceDescription := strings.TrimSpace(catalog.Description()); sourceDescription != "" {
			if strings.TrimSpace(description) == "" {
				description = sourceDescription
			} else {
				description = sourceDescription + "\n\n" + description
			}
		}
		attemptDeclaration, err := canonical.NewFunctionTool(
			function.Key(), description, function.InputSchema(), function.Strict(),
		)
		if err != nil {
			return nil, nil, nil, err
		}
		bindings[function.Key()] = binding{
			source: catalog.Key(), remoteName: function.Key().Name(),
		}
		attemptTools = append(attemptTools, attemptDeclaration)
	}
	var changes []compat.Change
	if strings.TrimSpace(catalog.Description()) != "" && len(bindings) > 0 {
		changes = append(changes, compat.Change{
			Capability: canonical.RequestTools,
			Kind:       compat.Approximation,
			Occurrence: canonical.ToolOccurrence(catalog.Key()),
			Preserved:  canonical.RequestTools,
		})
	}
	return bindings, attemptTools, changes, nil
}

// Calls purely classifies one completed assistant round. Budget reservation is
// deliberately separate so reducer evaluation does not hide state mutation.
func (r *Run) Calls(response canonical.CanonicalResponse) ([]canonical.ToolCallItem, error) {
	if r == nil {
		return nil, nil
	}
	var remote []canonical.ToolCallItem
	callerOwned := 0
	for _, item := range response.Items() {
		call, ok := item.ToolCall()
		if !ok {
			continue
		}
		if _, owned := r.bindings[call.Tool()]; owned {
			remote = append(remote, call)
			continue
		}
		if callerExecutes(call) {
			callerOwned++
		}
	}
	if len(remote) > 0 && callerOwned > 0 {
		return nil, canonical.NotImplemented("Swobu does not implement provider turns that mix remote MCP and caller-owned tool calls")
	}
	return remote, nil
}

func callerExecutes(call canonical.ToolCallItem) bool {
	switch call.Tool().Kind() {
	case canonical.ToolKindFunction, canonical.ToolKindCustom:
		return true
	case canonical.ToolKindDiscovery:
		executor, ok := call.DiscoveryExecutor()
		return !ok || executor == canonical.DiscoveryExecutorClient
	case canonical.ToolKindWebSearch:
		return false
	default:
		return false
	}
}

func (*Run) String() string   { return "<mcp-run>" }
func (*Run) GoString() string { return "<mcp-run>" }

// BeginBatch reserves one classified batch's side-effect budget. Exchange calls
// it only from command execution immediately before the first remote call.
func (r *Run) BeginBatch(calls []canonical.ToolCallItem) error {
	if r == nil {
		return canonical.InternalError("MCP batch requires an open runtime")
	}
	if len(calls) == 0 {
		return canonical.InternalError("MCP batch is empty")
	}
	for _, call := range calls {
		if _, ok := r.bindings[call.Tool()]; !ok {
			return canonical.InternalError("MCP batch contains a call not owned by this runtime")
		}
	}
	if r.roundCount >= MaxRemoteRoundsPerRun || r.callCount+len(calls) > MaxRemoteCallsPerRun {
		return canonical.NewBackendError("mcp", http.StatusBadGateway, "remote MCP execution exceeded the exchange side-effect budget", "")
	}
	r.roundCount++
	r.callCount += len(calls)
	return nil
}

// CanExecute reports whether this runtime exposes any callable MCP binding.
// An unavailable-only runtime still filters attempts but does not delay handoff.
func (r *Run) CanExecute() bool {
	return r != nil && len(r.bindings) > 0
}

// AttemptRequest applies the runtime's immutable availability and binding plan
// to a selected request segment without resolving catalogs again.
func (r *Run) AttemptRequest(request canonical.CanonicalRequest) (canonical.CanonicalRequest, error) {
	if r == nil {
		return request.Clone(), nil
	}
	return r.rewriteAttempt(request)
}

func (r *Run) rewriteAttempt(request canonical.CanonicalRequest) (canonical.CanonicalRequest, error) {
	return canonical.RewriteToolContributions(request, func(set canonical.ToolSet) (canonical.ToolSet, error) {
		projected := make([]canonical.ToolDeclaration, 0, len(set.Declarations()))
		for _, declaration := range set.Declarations() {
			namespace, ok := declaration.Namespace()
			if !ok {
				projected = append(projected, declaration.Clone())
				continue
			}
			if _, isMCP := namespace.MCPSource(); !isMCP {
				projected = append(projected, declaration.Clone())
				continue
			}
			if _, local := r.localSources[namespace.Key()]; !local {
				projected = append(projected, declaration.Clone())
				continue
			}
			for _, tool := range r.attemptTools[namespace.Key()] {
				projected = append(projected, tool.Clone())
			}
		}
		return canonical.NewToolSet(projected)
	})
}

func (r *Run) validateAttemptPolicy(attempt canonical.CanonicalRequest) error {
	environment, err := canonical.EffectiveTools(attempt)
	if err != nil {
		return err
	}
	policyErr := attempt.ToolPolicy().ValidateForTools(environment.Declarations())
	if policyErr == nil {
		return nil
	}
	if r.firstUnavailable != nil &&
		(attempt.ToolPolicy().Mode == canonical.ToolPolicyRequired ||
			attempt.ToolPolicy().Mode == canonical.ToolPolicySpecific) {
		return r.firstUnavailable
	}
	return policyErr
}

// Call executes one call already classified and budgeted as MCP-owned.
func (r *Run) Call(ctx context.Context, call canonical.ToolCallItem) (canonical.CanonicalItem, error) {
	if r == nil {
		return canonical.CanonicalItem{}, canonical.InternalError("MCP call requires an open runtime")
	}
	owned, ok := r.bindings[call.Tool()]
	if !ok {
		return canonical.CanonicalItem{}, canonical.InternalError("MCP call has no request-owned execution binding")
	}
	active := r.sessions[owned.source]
	if active == nil {
		return canonical.CanonicalItem{}, canonical.NewBackendError(
			"mcp:"+owned.source.Name(), http.StatusUnauthorized,
			"MCP source is not authorized for this request", "",
		)
	}
	input, ok := call.Input().Object()
	if !ok {
		return canonical.CanonicalItem{}, canonical.InternalError("MCP tool call input is not an object")
	}
	parts, isError, err := active.call(ctx, owned.remoteName, input)
	if err != nil {
		return canonical.CanonicalItem{}, dependencyError(active.source, err)
	}
	result, err := canonical.NewToolResultItem(call.CallID(), parts, isError)
	if err != nil {
		return canonical.CanonicalItem{}, canonical.InternalError(err.Error())
	}
	return result, nil
}

// Close releases every request-owned SDK session.
func (r *Run) Close() error {
	if r == nil {
		return nil
	}
	var joined error
	for _, active := range r.sessions {
		joined = errors.Join(joined, active.close())
	}
	r.sessions = nil
	return joined
}

func applyCatalogs(
	request canonical.CanonicalRequest,
	catalogs map[canonical.ToolKey]canonical.ToolNamespace,
) (canonical.CanonicalRequest, error) {
	return canonical.RewriteToolContributions(request, func(set canonical.ToolSet) (canonical.ToolSet, error) {
		declarations := set.Declarations()
		for index, declaration := range declarations {
			namespace, ok := declaration.Namespace()
			if !ok {
				continue
			}
			catalog, found := catalogs[namespace.Key()]
			if !found {
				continue
			}
			source, _ := catalog.MCPSource()
			replacement, err := canonical.NewMCPToolNamespace(
				catalog.Key(), catalog.Description(), source, catalog.Tools(),
			)
			if err != nil {
				return canonical.ToolSet{}, err
			}
			declarations[index] = replacement
		}
		return canonical.NewToolSet(declarations)
	})
}

func requestRequiresSource(
	request canonical.CanonicalRequest,
	environment canonical.ToolEnvironment,
	source canonical.ToolNamespace,
) bool {
	if specific, ok := request.ToolPolicy().SpecificID(); ok {
		return sourceOwnsTool(source, specific)
	}
	results := map[canonical.ToolCallID]struct{}{}
	for _, item := range request.Items() {
		if result, ok := item.ToolResult(); ok {
			results[result.CallID()] = struct{}{}
		}
	}
	for _, item := range request.Items() {
		call, ok := item.ToolCall()
		if !ok || !sourceOwnsTool(source, call.Tool()) {
			continue
		}
		if _, declared := environment.Lookup(call.Tool()); !declared {
			continue
		}
		if _, resolved := results[call.CallID()]; !resolved {
			return true
		}
	}
	return false
}

func sourceOwnsTool(source canonical.ToolNamespace, tool canonical.ToolKey) bool {
	return tool.Namespace() == source.Key().Namespace()+"/"+source.Key().Name()
}

func retainedCatalogBytes(catalog canonical.ToolNamespace) int {
	sourceDescription := catalog.Description()
	total := len(sourceDescription)
	for _, declaration := range catalog.Tools() {
		function, ok := declaration.Function()
		if !ok {
			continue
		}
		expandedDescription := function.Description()
		if sourceDescription != "" {
			expandedDescription = sourceDescription
			if function.Description() != "" {
				expandedDescription += "\n\n" + function.Description()
			}
		}
		total += len(function.Key().Name()) + len(function.Description()) +
			len(function.InputSchema().RawObject()) + len(expandedDescription)
	}
	return total
}

func droppedSourceDecision(key canonical.ToolKey) compat.Change {
	return compat.Change{
		Capability: canonical.RequestTools,
		Kind:       compat.Omission,
		Occurrence: canonical.ToolOccurrence(key),
	}
}

func sourceFailureIsInvariant(err error) bool {
	var canonicalError canonical.Error
	if !errors.As(err, &canonicalError) {
		return false
	}
	return canonicalError.Code == canonical.ErrorCodeBadRequest ||
		canonicalError.Code == canonical.ErrorCodeInternal
}

func mustMCPSource(namespace canonical.ToolNamespace) canonical.MCPSource {
	source, ok := namespace.MCPSource()
	if !ok {
		panic("MCP runtime received a non-MCP namespace")
	}
	return source
}

func sourceError(source canonical.ToolNamespace, err error) error {
	var canonicalError canonical.Error
	if errors.As(err, &canonicalError) {
		return err
	}
	return dependencyError(source, err)
}

func dependencyError(source canonical.ToolNamespace, err error) error {
	var canonicalError canonical.Error
	if errors.As(err, &canonicalError) {
		if canonicalError.Code == canonical.ErrorCodeNotImplemented ||
			canonicalError.Code == canonical.ErrorCodeInternal {
			return err
		}
	}
	var backendError canonical.BackendError
	if errors.As(err, &backendError) {
		backendError.Message = boundedDependencyEvidence(backendError.Message)
		return backendError
	}
	return canonical.NewBackendError(
		"mcp:"+source.Key().Name(), http.StatusBadGateway,
		boundedDependencyEvidence(err.Error()), "",
	)
}

func boundedDependencyEvidence(message string) string {
	if len(message) <= MaxDependencyEvidenceBytes {
		return message
	}
	limit := MaxDependencyEvidenceBytes
	for limit > 0 && !utf8.ValidString(message[:limit]) {
		limit--
	}
	return message[:limit]
}
