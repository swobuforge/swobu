// pass over AST and type info in a single semantic checker.
package rolelint

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var (
	weakGoBasenames        = []string{"types.go", "helpers.go", "support.go", "misc.go", "base.go", "common.go", "engine.go", "plan.go", "runtime.go", "kernel.go", "util.go", "utils.go", "shared.go"}
	weakConcreteNames      = []string{"Engine", "Runtime", "Kernel", "Helper", "Support", "Manager", "Service", "Controller", "Plan", "Base"}
	forbiddenNameLexemes   = []string{"kernel"}
	ignoredInterfaces      = []string{"Stringer", "error", "ViewSpec"}
	strictDataOnlySuffixes = []string{
		"Request", "Response", "Result",
		"Spec", "Snapshot", "State", "Record", "Entity",
		"DTO", "Document", "Payload", "Body",
		"Message", "Event", "Action",
		"Entry", "Item", "Option",
		"Config", "Profile", "Params", "Input", "Output",
	}
	passiveCarrierSuffixes = []string{
		"Request", "Response", "Result",
		"Spec", "Snapshot", "State", "Record", "Entity",
		"DTO", "Document", "Payload", "Body",
		"Message", "Event", "Action",
		"Entry", "Item", "Option",
		"Config", "Profile", "Params", "Input", "Output",
		"Requested", "Started", "Checked", "Loaded", "Stored", "Saved",
		"Noted", "Detected", "Failed", "Succeeded", "Tick",
		"Node", "Frame", "Ref", "Child", "Row", "Context", "Scope", "Props",
		"Envelope", "Fields", "Layout", "Placement", "Point", "Size", "Insets",
		"Style", "Cell", "Slot", "Policy", "Preset",
		"Contract", "Intent", "Plan", "Attempt", "Outcome", "Metadata", "Match",
		"Identity", "Provenance", "Capability", "Fact", "Counters",
		"Tuple", "Report", "Capture", "Wire",
		"Case", "Flow", "Session", "Version", "Evidence",
		"ID", "URL", "Key", "Name", "Alias",
		"Endpoint", "Endpoints", "Catalog", "Projection", "Status", "Model", "Mode", "Mismatch", "Target",
		"Filter", "Task", "Claim", "Class", "Info", "Constraint", "Constraints", "Effects", "Affordance", "Stream", "Block",
		"Doc", "Diagnostic", "Decision", "Candidate", "Call",
	}
	behaviorSuffixes        = []string{"Selector", "Resolver", "Classifier", "Executor", "Planner", "Mapper"}
	allowedValueMethodNames = map[string]struct{}{
		"Clone":  {},
		"String": {},
		"Len":    {},
		"Empty":  {},
		"Equal":  {},
		"Equals": {},
	}
	operationalMethodPrefixes = []string{
		"Build", "Execute", "Encode", "Decode", "Compile", "Apply",
		"Run", "Start", "Finish", "Load", "Save", "Dispatch",
		"Handle", "Render", "Resolve", "Select", "Plan", "Retry",
	}
)

// Analyzer enforces Swobu's concrete naming laws.
var Analyzer = &analysis.Analyzer{
	Name: "rolelint",
	Doc:  "enforce Swobu role-bearing naming conventions",
	Run:  run,
}

// report deduplication in one semantic pass over the package.
func run(pass *analysis.Pass) (any, error) {
	reportedFiles := make(map[string]bool)
	structs := make(map[structKey]structInfo)

	for _, file := range pass.Files {
		filename := pass.Fset.Position(file.Package).Filename
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		base := filepath.Base(filename)
		if isWeakGoBasename(base) && !reportedFiles[filename] {
			pass.Reportf(file.Package, "weak file basename %q; rename the file to its dominant concept or behavior", base)
			reportedFiles[filename] = true
		}
		if dominantType, ok := dominantExportedType(pass.Fset, file); ok && !hasDominantFilenameTokenOverlap(base, dominantType) {
			pass.Reportf(file.Package, "filename %q must share at least one exact token with dominant object %q", base, dominantType)
		}

		// Scan all comments in the file for the forbidden word.
		// Code marked for removal must be deleted, not annotated.
		for _, cg := range file.Comments {
			for _, c := range cg.List {
				// Skip analysistest "// want" directives — they must mention
				// the forbidden word to assert the diagnostic, but are not production code.
				if strings.HasPrefix(strings.TrimSpace(c.Text), "// want") {
					continue
				}
				text := strings.ToLower(c.Text)
				if strings.Contains(text, "deprecated") {
					pass.Reportf(c.Slash, "use of 'deprecated' in comments is forbidden; remove obsoleted code instead of annotating it")
				}
			}
		}
		for _, decl := range file.Decls {
			switch typed := decl.(type) {
			case *ast.FuncDecl:
				if typed.Name == nil {
					continue
				}
				if lexeme, ok := forbiddenIdentifierLexeme(typed.Name.Name); ok {
					pass.Reportf(typed.Name.Pos(), "identifier %q contains forbidden lexeme %q; rename using domain language", typed.Name.Name, lexeme)
				}
			case *ast.GenDecl:
				for _, spec := range typed.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name != nil {
							if lexeme, ok := forbiddenIdentifierLexeme(s.Name.Name); ok {
								pass.Reportf(s.Name.Pos(), "identifier %q contains forbidden lexeme %q; rename using domain language", s.Name.Name, lexeme)
							}
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n == nil {
								continue
							}
							if lexeme, ok := forbiddenIdentifierLexeme(n.Name); ok {
								pass.Reportf(n.Pos(), "identifier %q contains forbidden lexeme %q; rename using domain language", n.Name, lexeme)
							}
						}
					}
				}
			}
		}

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				named, ok := pass.TypesInfo.Defs[typeSpec.Name].Type().(*types.Named)
				if !ok {
					continue
				}
				if _, ok := named.Underlying().(*types.Struct); !ok {
					continue
				}
				key := makeStructKey(pass.Pkg.Path(), typeSpec.Name.Name)
				structs[key] = structInfo{
					Name:          typeSpec.Name.Name,
					Pos:           typeSpec.Name.Pos(),
					Location:      pass.Fset.Position(typeSpec.Name.Pos()),
					Type:          named,
					Score:         make(map[string]int),
					ExplicitScore: make(map[string]int),
				}
			}
		}
	}

	observeAssignments(pass, structs)
	observeValueSpecs(pass, structs)
	observeReturns(pass, structs)
	observeCalls(pass, structs)
	scoreImplicitInterfaces(pass, structs)

	for key, info := range structs {
		if slices.Contains(weakConcreteNames, info.Name) {
			pass.Reportf(info.Pos, "weak concrete struct name %q; rename it to reveal its dominant role", info.Name)
			continue
		}
		for _, msg := range structKindSuffixDiagnostics(info) {
			pass.Reportf(info.Pos, msg)
		}

		dominant, count, tied := dominantInterface(info.Score)
		if dominant == "" || count == 0 || tied {
			_ = key
			continue
		}
		if !containsNormalized(info.Name, dominant) {
			pass.Reportf(info.Pos, "struct %q is used most often as %q; include that interface noun in the concrete name", info.Name, dominant)
		}
	}

	return nil, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type structKey string

type structInfo struct {
	Name          string
	Pos           token.Pos
	Location      token.Position
	Type          *types.Named
	Score         map[string]int
	ExplicitScore map[string]int
}

func makeStructKey(pkgPath, name string) structKey {
	return structKey(pkgPath + "." + name)
}

func observeAssignments(pass *analysis.Pass, structs map[structKey]structInfo) {
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Package).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i := range min(len(assign.Lhs), len(assign.Rhs)) {
				countUsage(pass, structs, assign.Rhs[i], pass.TypesInfo.TypeOf(assign.Lhs[i]), true)
			}
			return true
		})
	}
}

func observeValueSpecs(pass *analysis.Pass, structs map[structKey]structInfo) {
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Package).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.ValueSpec)
			if !ok || spec.Type == nil {
				return true
			}
			target := pass.TypesInfo.TypeOf(spec.Type)
			for _, value := range spec.Values {
				countUsage(pass, structs, value, target, true)
			}
			return true
		})
	}
}

func observeReturns(pass *analysis.Pass, structs map[structKey]structInfo) {
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Package).Filename, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Type.Results == nil {
				continue
			}
			var resultTypes []types.Type
			for _, field := range fn.Type.Results.List {
				typ := pass.TypesInfo.TypeOf(field.Type)
				count := len(field.Names)
				if count == 0 {
					count = 1
				}
				for i := 0; i < count; i++ {
					resultTypes = append(resultTypes, typ)
				}
			}
			inspectReturns(fn.Body, func(ret *ast.ReturnStmt) {
				for i := range min(len(ret.Results), len(resultTypes)) {
					countUsage(pass, structs, ret.Results[i], resultTypes[i], true)
				}
			})
		}
	}
}

func observeCalls(pass *analysis.Pass, structs map[structKey]structInfo) {
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Package).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			calleeType := pass.TypesInfo.TypeOf(call.Fun)
			sig, ok := unwrapSignature(calleeType)
			if !ok {
				return true
			}
			for i, arg := range call.Args {
				paramType, ok := paramType(sig, i)
				if !ok {
					continue
				}
				countUsage(pass, structs, arg, paramType, true)
			}
			return true
		})
	}
}

func scoreImplicitInterfaces(pass *analysis.Pass, structs map[structKey]structInfo) {
	candidates := repoInterfaces(pass)
	implCount := make(map[string]int)
	for _, info := range structs {
		for _, candidate := range candidates {
			if candidate.Name == "" || candidate.Type == nil {
				continue
			}
			if implements(info.Type, candidate.Type) {
				implCount[candidate.Name]++
			}
		}
	}
	for key, info := range structs {
		for _, candidate := range candidates {
			if candidate.Name == "" || candidate.Type == nil {
				continue
			}
			if implements(info.Type, candidate.Type) {
				if isIgnoredInterface(candidate.Name) {
					continue
				}
				info.Score[candidate.Name] += implCount[candidate.Name]
			}
		}
		structs[key] = info
	}
}

func countUsage(pass *analysis.Pass, structs map[structKey]structInfo, value ast.Expr, target types.Type, explicit bool) {
	structType := namedStruct(pass.TypesInfo.TypeOf(value))
	if structType == nil {
		return
	}
	interfaceName, ok := roleInterfaceName(target)
	if !ok {
		return
	}
	key := makeStructKey(structType.Obj().Pkg().Path(), structType.Obj().Name())
	info, ok := structs[key]
	if !ok {
		return
	}
	if !isIgnoredInterface(interfaceName) {
		info.Score[interfaceName]++
	}
	if explicit {
		info.ExplicitScore[interfaceName]++
	}
	structs[key] = info
}

func repoInterfaces(pass *analysis.Pass) []interfaceCandidate {
	seen := make(map[string]bool)
	var out []interfaceCandidate
	addFromScope := func(pkgPath string, scope *types.Scope, includeUnexported bool) {
		if scope == nil {
			return
		}
		for _, name := range scope.Names() {
			if !includeUnexported && !token.IsExported(name) {
				continue
			}
			obj := scope.Lookup(name)
			typeName, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := typeName.Type().(*types.Named)
			if !ok {
				continue
			}
			iface, ok := named.Underlying().(*types.Interface)
			if !ok || iface.NumMethods() == 0 {
				continue
			}
			key := pkgPath + "." + name
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, interfaceCandidate{Name: name, Type: named})
		}
	}

	addFromScope(pass.Pkg.Path(), pass.Pkg.Scope(), true)
	for _, imported := range pass.Pkg.Imports() {
		addFromScope(imported.Path(), imported.Scope(), false)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type interfaceCandidate struct {
	Name string
	Type *types.Named
}

func namedStruct(typ types.Type) *types.Named {
	switch typed := typ.(type) {
	case *types.Named:
		if _, ok := typed.Underlying().(*types.Struct); ok {
			return typed
		}
	case *types.Pointer:
		if named, ok := typed.Elem().(*types.Named); ok {
			if _, ok := named.Underlying().(*types.Struct); ok {
				return named
			}
		}
	}
	return nil
}

func roleInterfaceName(typ types.Type) (string, bool) {
	switch typed := typ.(type) {
	case *types.Named:
		if _, ok := typed.Underlying().(*types.Interface); !ok {
			return "", false
		}
		if typed.Obj() == nil {
			return "", false
		}
		return typed.Obj().Name(), true
	case *types.Interface:
		return "", false
	default:
		return "", false
	}
}

func implements(named *types.Named, ifaceNamed *types.Named) bool {
	iface, ok := ifaceNamed.Underlying().(*types.Interface)
	if !ok {
		return false
	}
	if types.Implements(named, iface) || types.Implements(types.NewPointer(named), iface) {
		return true
	}
	// Generic interface: fall back to structural method matching.
	// When the interface has type parameters, types.Implements fails against
	// concrete implementors that bind specific type arguments.
	if ifaceNamed.TypeParams() != nil && ifaceNamed.TypeParams().Len() > 0 {
		return structurallyImplements(named, iface)
	}
	return false
}

// structurallyImplements checks if a concrete type has methods matching all
// interface methods by name (ignoring type parameters).
func structurallyImplements(named *types.Named, iface *types.Interface) bool {
	methodSet := types.NewMethodSet(types.NewPointer(named))
	for i := 0; i < iface.NumMethods(); i++ {
		ifaceMethod := iface.Method(i)
		found := false
		for j := 0; j < methodSet.Len(); j++ {
			if methodSet.At(j).Obj().Name() == ifaceMethod.Name() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func unwrapSignature(typ types.Type) (*types.Signature, bool) {
	switch typed := typ.(type) {
	case *types.Signature:
		return typed, true
	case *types.Named:
		if sig, ok := typed.Underlying().(*types.Signature); ok {
			return sig, true
		}
	}
	return nil, false
}

func paramType(sig *types.Signature, index int) (types.Type, bool) {
	params := sig.Params()
	if params == nil || params.Len() == 0 {
		return nil, false
	}
	if sig.Variadic() && index >= params.Len()-1 {
		if slice, ok := params.At(params.Len() - 1).Type().(*types.Slice); ok {
			return slice.Elem(), true
		}
	}
	if index >= params.Len() {
		return nil, false
	}
	return params.At(index).Type(), true
}

func inspectReturns(node ast.Node, visit func(*ast.ReturnStmt)) {
	ast.Inspect(node, func(current ast.Node) bool {
		switch typed := current.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			visit(typed)
		}
		return true
	})
}

func dominantInterface(scores map[string]int) (name string, count int, tied bool) {
	for candidate, score := range scores {
		if score > count {
			name, count, tied = candidate, score, false
			continue
		}
		if score == count && score > 0 && candidate != name {
			tied = true
		}
	}
	return name, count, tied
}

func containsNormalized(name, interfaceName string) bool {
	return normalizeIdentifier(name) == normalizeIdentifier(interfaceName) || strings.Contains(normalizeIdentifier(name), normalizeIdentifier(interfaceName))
}

func isIgnoredInterface(name string) bool {
	for _, ignored := range ignoredInterfaces {
		if name == ignored {
			return true
		}
	}
	return false
}

func normalizeIdentifier(name string) string {
	var out strings.Builder
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r + ('a' - 'A'))
		case r == '_' || r == '-':
			continue
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func structKindSuffixDiagnostics(info structInfo) []string {
	var diagnostics []string
	hasStrictDataSuffix := hasAnySuffix(info.Name, strictDataOnlySuffixes)
	hasPassiveCarrierSuffix := hasAnySuffix(info.Name, passiveCarrierSuffixes)
	hasBehaviorSuffix := hasAnySuffix(info.Name, behaviorSuffixes)

	if info.Type != nil && info.Type.NumMethods() == 0 && !hasPassiveCarrierSuffix && !hasBehaviorSuffix {
		diagnostics = append(diagnostics, "no-method struct "+strconvQuote(info.Name)+" must use a data suffix ("+strings.Join(passiveCarrierSuffixes, ", ")+"); if this is a valid passive data noun, extend rolelint data-only suffixes")
	}
	if hasStrictDataSuffix && info.Type != nil && info.Type.NumMethods() > 0 {
		operationalMethods := operationalMethodsOnDataType(info.Type)
		if len(operationalMethods) > 0 {
			diagnostics = append(diagnostics, "data-suffix struct "+strconvQuote(info.Name)+" must not declare operational methods; use behavior-owner naming or move behavior out; operational methods found: "+strings.Join(operationalMethods, ", "))
		}
	}
	if hasBehaviorSuffix && (info.Type == nil || info.Type.NumMethods() == 0) {
		diagnostics = append(diagnostics, "behavior-suffix struct "+strconvQuote(info.Name)+" must declare at least one method")
	}
	return diagnostics
}

func operationalMethodsOnDataType(named *types.Named) []string {
	if named == nil {
		return nil
	}
	names := make([]string, 0, named.NumMethods())
	for i := 0; i < named.NumMethods(); i++ {
		method := named.Method(i)
		sig, ok := method.Type().(*types.Signature)
		if !ok {
			names = append(names, method.Name())
			continue
		}
		if !isValueSemanticMethod(method.Name(), sig, named) {
			names = append(names, method.Name())
		}
	}
	sort.Strings(names)
	return names
}

func isValueSemanticMethod(name string, sig *types.Signature, receiver *types.Named) bool {
	if _, ok := allowedValueMethodNames[name]; ok {
		return true
	}
	if strings.HasPrefix(name, "Has") && boolNoArgMethod(sig) {
		return true
	}
	if strings.HasPrefix(name, "Is") && boolNoArgMethod(sig) {
		return true
	}
	if strings.HasPrefix(name, "With") && immutableWitherMethod(sig, receiver) {
		return true
	}
	if accessorLikeMethod(sig) && !hasOperationalPrefix(name) {
		return true
	}
	return false
}

func hasOperationalPrefix(name string) bool {
	for _, prefix := range operationalMethodPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func boolNoArgMethod(sig *types.Signature) bool {
	if sig == nil || tupleLen(sig.Params()) != 0 || tupleLen(sig.Results()) != 1 {
		return false
	}
	basic, ok := sig.Results().At(0).Type().Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Bool
}

func accessorLikeMethod(sig *types.Signature) bool {
	if sig == nil || tupleLen(sig.Params()) != 0 {
		return false
	}
	switch tupleLen(sig.Results()) {
	case 1:
		return true
	case 2:
		second := sig.Results().At(1).Type()
		if isErrorType(second) {
			return true
		}
		basic, ok := second.Underlying().(*types.Basic)
		return ok && basic.Kind() == types.Bool
	default:
		return false
	}
}

func immutableWitherMethod(sig *types.Signature, receiver *types.Named) bool {
	if sig == nil || receiver == nil || tupleLen(sig.Params()) == 0 || tupleLen(sig.Results()) == 0 {
		return false
	}
	first := sig.Results().At(0).Type()
	if !sameNamedOrPointer(first, receiver) {
		return false
	}
	switch tupleLen(sig.Results()) {
	case 1:
		return true
	case 2:
		return isErrorType(sig.Results().At(1).Type())
	default:
		return false
	}
}

func tupleLen(t *types.Tuple) int {
	if t == nil {
		return 0
	}
	return t.Len()
}

func forbiddenIdentifierLexeme(name string) (string, bool) {
	tokens := tokenizeName(name)
	if len(tokens) == 0 {
		return "", false
	}
	for _, token := range tokens {
		for _, banned := range forbiddenNameLexemes {
			if token == banned {
				return banned, true
			}
		}
	}
	return "", false
}

func sameNamedOrPointer(t types.Type, named *types.Named) bool {
	switch typed := t.(type) {
	case *types.Named:
		return typed.Obj() != nil && named.Obj() != nil &&
			typed.Obj().Pkg() != nil && named.Obj().Pkg() != nil &&
			typed.Obj().Pkg().Path() == named.Obj().Pkg().Path() &&
			typed.Obj().Name() == named.Obj().Name()
	case *types.Pointer:
		elem, ok := typed.Elem().(*types.Named)
		if !ok {
			return false
		}
		return sameNamedOrPointer(elem, named)
	default:
		return false
	}
}

func isErrorType(t types.Type) bool {
	builtinErr := types.Universe.Lookup("error")
	if builtinErr == nil {
		return false
	}
	return types.Identical(t, builtinErr.Type())
}

func hasAnySuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func strconvQuote(v string) string {
	return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
}
