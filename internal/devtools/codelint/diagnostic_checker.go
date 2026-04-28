package codelint

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
)

const (
	fileLinesThreshold          = 400
	functionLinesThreshold      = 160
	functionComplexityThreshold = 24
)

const (
	ruleFileLength      = "file-length"
	ruleFunctionLength  = "function-length"
	ruleComplexity      = "complexity"
	ruleStaleReferences = "stale-references"
	ruleTUILayoutImport = "tui-userspace-layout-import"
	ruleTUILayoutAPI    = "tui-userspace-layout-api"
	ruleTUIRenderShape  = "tui-userspace-render-shape"
	ruleTUILayoutSkin   = "tui-userspace-layout-skin"
	ruleTUIGeometryCall = "tui-userspace-geometry-call"
	ruleTUIClipboardCmd = "tui-clipboard-command-probe"
	ruleRedundantTrim   = "redundant-model-trim"
	ruleLongSleep       = "long-sleep"
	ruleNoEmptyIface    = "no-empty-interface"
	ruleCanonicalType   = "canonical-semantic-type-name"
	ignoreMarker        = "swobu:codelint ignore "
)

type Diagnostic struct {
	Filename string
	Line     int
	Column   int
	Message  string
}

func Check(patterns []string) ([]Diagnostic, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return checkDir(dir, patterns)
}

func checkDir(dir string, patterns []string) ([]Diagnostic, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Dir:  dir,
		Fset: fset,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedSyntax,
		Tests: false,
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}

	var diagnostics []Diagnostic
	for _, pkg := range pkgs {
		diagnostics = append(diagnostics, packageLoadDiagnostics(pkg)...)
		for _, file := range pkg.Syntax {
			filename := pkg.Fset.Position(file.Package).Filename
			if shouldSkipFile(filename, file) {
				continue
			}
			fileDirectives := fileIgnoreSet(pkg.Fset, file)
			diagnostics = append(diagnostics, fileSizeDiagnostics(pkg.Fset, file, fileDirectives)...)
			diagnostics = append(diagnostics, functionDiagnostics(pkg.Fset, file)...)
			diagnostics = append(diagnostics, staleReferenceDiagnostics(pkg.Fset, file, fileDirectives)...)
			diagnostics = append(diagnostics, tuiUserspaceDiagnostics(pkg.Fset, file, filename, fileDirectives)...)
			diagnostics = append(diagnostics, tuiClipboardCommodityDiagnostics(pkg.Fset, file, filename, fileDirectives)...)
			diagnostics = append(diagnostics, redundantModelTrimDiagnostics(pkg.Fset, file, filename, fileDirectives)...)
			diagnostics = append(diagnostics, longSleepDiagnostics(pkg.Fset, file, filename, fileDirectives)...)
			diagnostics = append(diagnostics, noEmptyInterfaceDiagnostics(pkg.Fset, file, filename, fileDirectives)...)
			diagnostics = append(diagnostics, canonicalSemanticTypeNameDiagnostics(pkg.Fset, file, filename, fileDirectives)...)
		}
	}

	slices.SortFunc(diagnostics, func(a, b Diagnostic) int {
		if a.Filename != b.Filename {
			return strings.Compare(a.Filename, b.Filename)
		}
		if a.Line != b.Line {
			if a.Line < b.Line {
				return -1
			}
			return 1
		}
		if a.Column != b.Column {
			if a.Column < b.Column {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Message, b.Message)
	})
	// Deduplicate: packages.Load can revisit the same file via multiple packages.
	deduped := make([]Diagnostic, 0, len(diagnostics))
	for i, d := range diagnostics {
		if i == 0 {
			deduped = append(deduped, d)
			continue
		}
		prev := deduped[len(deduped)-1]
		if d.Filename == prev.Filename && d.Line == prev.Line && d.Column == prev.Column && d.Message == prev.Message {
			continue
		}
		deduped = append(deduped, d)
	}
	return deduped, nil
}

func staleReferenceDiagnostics(fset *token.FileSet, file *ast.File, fileDirectives map[string]bool) []Diagnostic {
	var diagnostics []Diagnostic
	if fileDirectives[ruleStaleReferences] {
		pos := fset.PositionFor(file.Package, false)
		diagnostics = append(diagnostics, Diagnostic{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  "stale-reference rule does not support swobu:codelint ignore; remove ignore and remove legacy/deprecated references",
		})
	}

	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")
		term := firstStaleTerm(path)
		if term == "" {
			continue
		}
		pos := fset.PositionFor(imp.Pos(), false)
		diagnostics = append(diagnostics, Diagnostic{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  fmt.Sprintf("import path references forbidden stale term %q; use evergreen package surfaces", term),
		})
	}

	for _, commentGroup := range file.Comments {
		for _, comment := range commentGroup.List {
			term := firstStaleTerm(comment.Text)
			if term == "" {
				continue
			}
			pos := fset.PositionFor(comment.Pos(), false)
			diagnostics = append(diagnostics, Diagnostic{
				Filename: pos.Filename,
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  fmt.Sprintf("comment references forbidden stale term %q; rewrite to evergreen language", term),
			})
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		raw := strings.Trim(lit.Value, "\"`")
		term := firstStaleTerm(raw)
		if term == "" {
			return true
		}
		pos := fset.PositionFor(lit.Pos(), false)
		diagnostics = append(diagnostics, Diagnostic{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  fmt.Sprintf("string literal references forbidden stale term %q; remove stale vocabulary from production code", term),
		})
		return true
	})

	return diagnostics
}

func firstStaleTerm(input string) string {
	lower := strings.ToLower(input)
	switch {
	case strings.Contains(lower, "legacy"):
		return "legacy"
	case strings.Contains(lower, "deprecated"):
		return "deprecated"
	default:
		return ""
	}
}

func tuiClipboardCommodityDiagnostics(fset *token.FileSet, file *ast.File, filename string, fileDirectives map[string]bool) []Diagnostic {
	if !isTUIClipboardFile(filename) {
		return nil
	}
	pos := fset.PositionFor(file.Package, false)
	if fileDirectives[ruleTUIClipboardCmd] {
		return []Diagnostic{{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  "tui clipboard commodity rule does not support swobu:codelint ignore; remove ignore and use golang.design/x/clipboard",
		}}
	}
	if hasCommandProbeClipboardPattern(file) {
		return []Diagnostic{{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  "tui clipboard writes must use golang.design/x/clipboard; command-probed clipboard implementations are forbidden",
		}}
	}
	return nil
}

func isTUIClipboardFile(filename string) bool {
	return strings.Contains(filename, "/internal/adapters/inbound/tui/") &&
		strings.HasSuffix(filename, "/clipboard.go")
}

func hasCommandProbeClipboardPattern(file *ast.File) bool {
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")
		if path == "os/exec" {
			return true
		}
	}
	commandLiterals := map[string]struct{}{
		"pbcopy":         {},
		"wl-copy":        {},
		"xclip":          {},
		"xsel":           {},
		"clip":           {},
		"powershell":     {},
		"powershell.exe": {},
		"pwsh":           {},
		"pwsh.exe":       {},
		"Set-Clipboard":  {},
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if found {
			return false
		}
		switch n := node.(type) {
		case *ast.BasicLit:
			if n.Kind != token.STRING {
				return true
			}
			value := strings.Trim(n.Value, "\"`")
			if _, ok := commandLiterals[value]; ok {
				found = true
				return false
			}
		case *ast.SelectorExpr:
			id, ok := n.X.(*ast.Ident)
			if !ok || id.Name != "exec" || n.Sel == nil {
				return true
			}
			if n.Sel.Name == "LookPath" || n.Sel.Name == "Command" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func redundantModelTrimDiagnostics(fset *token.FileSet, file *ast.File, filename string, fileDirectives map[string]bool) []Diagnostic {
	if !isTUIViewOrSelectorFile(filename) {
		return nil
	}
	if fileDirectives[ruleRedundantTrim] {
		return nil
	}

	var diagnostics []Diagnostic
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Check for strings.TrimSpace(...)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "TrimSpace" {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != "strings" {
			return true
		}

		// Check if the argument is a model field access
		if len(call.Args) != 1 {
			return true
		}
		arg := call.Args[0]
		if isModelFieldAccess(arg) {
			pos := fset.PositionFor(call.Pos(), false)
			diagnostics = append(diagnostics, Diagnostic{
				Filename: pos.Filename,
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  "redundant strings.TrimSpace on model field; trim at write boundary (reducers/effects), not on read",
			})
		}
		return true
	})
	return diagnostics
}

func longSleepDiagnostics(fset *token.FileSet, file *ast.File, filename string, fileDirectives map[string]bool) []Diagnostic {
	if strings.Contains(filename, "/legacy/") {
		return nil
	}
	if fileDirectives[ruleLongSleep] {
		return nil
	}

	var diagnostics []Diagnostic
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || !isTimeSleepCall(call.Fun) {
			return true
		}
		duration, ok := durationLiteral(call.Args[0])
		if !ok || duration < time.Second {
			return true
		}
		pos := fset.PositionFor(call.Pos(), false)
		diagnostics = append(diagnostics, Diagnostic{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  "time.Sleep >= 1s is forbidden; prefer event-driven waits or short polling intervals",
		})
		return true
	})
	return diagnostics
}

func noEmptyInterfaceDiagnostics(fset *token.FileSet, file *ast.File, filename string, fileDirectives map[string]bool) []Diagnostic {
	if strings.Contains(filename, "/legacy/") {
		return nil
	}
	if fileDirectives[ruleNoEmptyIface] {
		return nil
	}

	var diagnostics []Diagnostic
	ast.Inspect(file, func(node ast.Node) bool {
		typeNode, ok := node.(*ast.InterfaceType)
		if !ok {
			return true
		}
		if typeNode.Methods == nil || len(typeNode.Methods.List) == 0 {
			pos := fset.PositionFor(typeNode.Pos(), false)
			diagnostics = append(diagnostics, Diagnostic{
				Filename: pos.Filename,
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  "empty interface type is forbidden; use concrete type or `any` alias intentionally",
			})
		}
		return true
	})
	return diagnostics
}

func canonicalSemanticTypeNameDiagnostics(fset *token.FileSet, file *ast.File, filename string, fileDirectives map[string]bool) []Diagnostic {
	if !isCanonicalDomainFile(filename) {
		return nil
	}
	pos := fset.PositionFor(file.Package, false)
	if fileDirectives[ruleCanonicalType] {
		return []Diagnostic{{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  "canonical semantic type naming rule does not support swobu:codelint ignore; remove ignore and use semantic type names",
		}}
	}

	var diagnostics []Diagnostic
	ast.Inspect(file, func(node ast.Node) bool {
		gen, ok := node.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			return true
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name == nil {
				continue
			}
			name := strings.TrimSpace(typeSpec.Name.Name)
			if name == "" || !strings.HasSuffix(name, "Request") {
				continue
			}
			if token := firstProtocolToken(name); token != "" {
				typePos := fset.PositionFor(typeSpec.Name.Pos(), false)
				diagnostics = append(diagnostics, Diagnostic{
					Filename: typePos.Filename,
					Line:     typePos.Line,
					Column:   typePos.Column,
					Message:  fmt.Sprintf("canonical semantic type name %q embeds protocol token %q; use semantic naming independent of protocol vocabulary", name, token),
				})
			}
		}
		return true
	})
	return diagnostics
}

func isCanonicalDomainFile(filename string) bool {
	return strings.Contains(filename, "/internal/domain/compatibility/")
}

func firstProtocolToken(typeName string) string {
	tokens := []string{"Responses", "ChatCompletions", "Messages", "Completions"}
	for _, token := range tokens {
		if strings.Contains(typeName, token) {
			return token
		}
	}
	return ""
}

func isTimeSleepCall(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "Sleep" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "time"
}

func durationLiteral(expr ast.Expr) (time.Duration, bool) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return durationLiteral(e.X)
	case *ast.SelectorExpr:
		id, ok := e.X.(*ast.Ident)
		if !ok || id.Name != "time" || e.Sel == nil {
			return 0, false
		}
		switch e.Sel.Name {
		case "Nanosecond":
			return time.Nanosecond, true
		case "Microsecond":
			return time.Microsecond, true
		case "Millisecond":
			return time.Millisecond, true
		case "Second":
			return time.Second, true
		case "Minute":
			return time.Minute, true
		case "Hour":
			return time.Hour, true
		default:
			return 0, false
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.MUL:
			leftDuration, leftIsDuration := durationLiteral(e.X)
			rightDuration, rightIsDuration := durationLiteral(e.Y)
			leftScalar, leftIsScalar := numericLiteral(e.X)
			rightScalar, rightIsScalar := numericLiteral(e.Y)
			if leftIsDuration && rightIsScalar {
				return time.Duration(float64(leftDuration) * rightScalar), true
			}
			if rightIsDuration && leftIsScalar {
				return time.Duration(float64(rightDuration) * leftScalar), true
			}
			if leftIsDuration && rightIsDuration {
				return 0, false
			}
		case token.ADD:
			left, okLeft := durationLiteral(e.X)
			right, okRight := durationLiteral(e.Y)
			if okLeft && okRight {
				return left + right, true
			}
		case token.SUB:
			left, okLeft := durationLiteral(e.X)
			right, okRight := durationLiteral(e.Y)
			if okLeft && okRight {
				return left - right, true
			}
		}
	}
	return 0, false
}

func numericLiteral(expr ast.Expr) (float64, bool) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return numericLiteral(e.X)
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT:
			v, err := strconv.ParseInt(e.Value, 10, 64)
			if err != nil {
				return 0, false
			}
			return float64(v), true
		case token.FLOAT:
			v, err := strconv.ParseFloat(e.Value, 64)
			if err != nil {
				return 0, false
			}
			return v, true
		default:
			return 0, false
		}
	case *ast.UnaryExpr:
		v, ok := numericLiteral(e.X)
		if !ok {
			return 0, false
		}
		switch e.Op {
		case token.ADD:
			return v, true
		case token.SUB:
			return -v, true
		default:
			return 0, false
		}
	case *ast.CallExpr:
		// Handle casts like time.Duration(2) in multiplications.
		if len(e.Args) != 1 {
			return 0, false
		}
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			id, okID := sel.X.(*ast.Ident)
			if !okID || id.Name != "time" || sel.Sel == nil || sel.Sel.Name != "Duration" {
				return 0, false
			}
			return numericLiteral(e.Args[0])
		}
	}
	return 0, false
}

func isTUIViewOrSelectorFile(filename string) bool {
	return strings.Contains(filename, "/internal/adapters/inbound/tui/app/views/") ||
		strings.Contains(filename, "/internal/adapters/inbound/tui/app/selectors/")
}

func isModelFieldAccess(expr ast.Node) bool {
	switch n := expr.(type) {
	case *ast.SelectorExpr:
		// Check if it's model.FieldName or model.Field.NestedField
		x, ok := n.X.(*ast.Ident)
		if ok && x.Name == "model" {
			return true
		}
		// Check for model.Field.NestedField (e.g., model.CreateDraftProviderConfig.ProviderSpec)
		if inner, ok := n.X.(*ast.SelectorExpr); ok {
			if innerX, ok := inner.X.(*ast.Ident); ok && innerX.Name == "model" {
				return true
			}
		}
	case *ast.Ident:
		// Direct model variable access
		if n.Name == "model" {
			return true
		}
	}
	return false
}

func packageLoadDiagnostics(pkg *packages.Package) []Diagnostic {
	if len(pkg.Errors) == 0 {
		return nil
	}
	out := make([]Diagnostic, 0, len(pkg.Errors))
	for _, pkgErr := range pkg.Errors {
		out = append(out, Diagnostic{Filename: pkgErr.Pos, Message: pkgErr.Msg})
	}
	return out
}

func fileSizeDiagnostics(fset *token.FileSet, file *ast.File, fileDirectives map[string]bool) []Diagnostic {
	if fileDirectives[ruleFileLength] {
		return nil
	}
	fileInfo := fset.File(file.Pos())
	if fileInfo == nil {
		return nil
	}
	lineCount := fileInfo.LineCount()
	if lineCount <= fileLinesThreshold {
		return nil
	}
	pos := fset.PositionFor(file.Package, false)
	return []Diagnostic{{
		Filename: pos.Filename,
		Line:     pos.Line,
		Column:   pos.Column,
		Message:  fmt.Sprintf("file has %d lines and exceeds the repo cap (%d)", lineCount, fileLinesThreshold),
	}}
}

func functionDiagnostics(fset *token.FileSet, file *ast.File) []Diagnostic {
	var diagnostics []Diagnostic
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ignores := commentGroupIgnoreSet(fn.Doc)
		start := fset.PositionFor(fn.Pos(), false)
		end := fset.PositionFor(fn.End(), false)
		lineCount := end.Line - start.Line + 1
		if !ignores[ruleFunctionLength] && lineCount > functionLinesThreshold {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: start.Filename,
				Line:     start.Line,
				Column:   start.Column,
				Message:  fmt.Sprintf("function %q has %d lines and exceeds the repo cap (%d)", fn.Name.Name, lineCount, functionLinesThreshold),
			})
		}
		complexity := cyclomaticComplexity(fn)
		if !ignores[ruleComplexity] && complexity > functionComplexityThreshold {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: start.Filename,
				Line:     start.Line,
				Column:   start.Column,
				Message:  fmt.Sprintf("function %q has cyclomatic complexity %d and exceeds the repo cap (%d)", fn.Name.Name, complexity, functionComplexityThreshold),
			})
		}
	}
	return diagnostics
}

func tuiUserspaceDiagnostics(fset *token.FileSet, file *ast.File, filename string, fileDirectives map[string]bool) []Diagnostic {
	if !isTUIAppUserspaceFile(filename) {
		return nil
	}
	isShellExceptionFile := isTUIAppShellExceptionFile(filename)
	var diagnostics []Diagnostic
	if !(isShellExceptionFile && fileDirectives[ruleTUILayoutImport]) {
		diagnostics = append(diagnostics, tuiUserspaceLayoutImportDiagnostics(fset, file)...)
	}
	if !(isShellExceptionFile && fileDirectives[ruleTUILayoutAPI]) {
		diagnostics = append(diagnostics, tuiUserspaceLayoutAPIDiagnostics(fset, file)...)
	}
	if !(isShellExceptionFile && fileDirectives[ruleTUIRenderShape]) {
		diagnostics = append(diagnostics, tuiUserspaceBuildSignatureDiagnostics(fset, file)...)
	}
	if !(isShellExceptionFile && fileDirectives[ruleTUILayoutSkin]) {
		diagnostics = append(diagnostics, tuiUserspaceLayoutSkinDiagnostics(fset, file)...)
	}
	if !(isShellExceptionFile && fileDirectives[ruleTUIGeometryCall]) {
		diagnostics = append(diagnostics, tuiUserspaceGeometryCallDiagnostics(fset, file)...)
	}

	if !isShellExceptionFile && (fileDirectives[ruleTUILayoutImport] || fileDirectives[ruleTUILayoutAPI] || fileDirectives[ruleTUIRenderShape] || fileDirectives[ruleTUILayoutSkin] || fileDirectives[ruleTUIGeometryCall]) {
		pos := fset.PositionFor(file.Package, false)
		diagnostics = append(diagnostics, Diagnostic{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  "tui userspace boundary rules do not support swobu:codelint ignore; remove ignore and fix code",
		})
	}
	return diagnostics
}

func tuiUserspaceLayoutSkinDiagnostics(fset *token.FileSet, file *ast.File) []Diagnostic {
	var diagnostics []Diagnostic
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		_, selector := selectorNameFromExpr(call.Fun)
		if selector != "ListItemRow" && selector != "ListItemRowWithHooks" && selector != "InsetLabel" {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		raw, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if raw == strings.TrimSpace(raw) {
			return true
		}
		pos := fset.PositionFor(lit.Pos(), false)
		diagnostics = append(diagnostics, Diagnostic{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  "tui semantic row composition must not pass layout skin literals with leading/trailing whitespace; use typed composition helpers",
		})
		return true
	})
	return diagnostics
}

func selectorNameFromExpr(expr ast.Expr) (string, string) {
	switch v := expr.(type) {
	case *ast.SelectorExpr:
		if ident, ok := v.X.(*ast.Ident); ok {
			return ident.Name, v.Sel.Name
		}
		return "", v.Sel.Name
	case *ast.IndexExpr:
		return selectorNameFromExpr(v.X)
	case *ast.IndexListExpr:
		return selectorNameFromExpr(v.X)
	default:
		return "", ""
	}
}

func tuiUserspaceGeometryCallDiagnostics(fset *token.FileSet, file *ast.File) []Diagnostic {
	var diagnostics []Diagnostic
	forbidden := map[string]struct{}{
		"Padded":    {},
		"Inset":     {},
		"Constrain": {},
		"ScrollY":   {},
		"Grow":      {},
		"Column":    {},
		"ColumnGap": {},
		"Row":       {},
		"RowGap":    {},
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		pkg, selector := selectorNameFromExpr(call.Fun)
		if pkg != "view" {
			return true
		}
		if _, banned := forbidden[selector]; !banned {
			return true
		}
		pos := fset.PositionFor(call.Pos(), false)
		diagnostics = append(diagnostics, Diagnostic{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  "tui app userspace must compose geometry with view-transform helpers (view.With*); direct view geometry wrappers are forbidden",
		})
		return true
	})
	return diagnostics
}

func isTUIAppUserspaceFile(filename string) bool {
	return strings.Contains(filename, "/internal/adapters/inbound/tui/app/")
}

func isTUIAppShellExceptionFile(filename string) bool {
	return strings.HasSuffix(filename, "/internal/adapters/inbound/tui/app/views/shell.go")
}

func tuiUserspaceLayoutImportDiagnostics(fset *token.FileSet, file *ast.File) []Diagnostic {
	var diagnostics []Diagnostic
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")
		if !strings.HasSuffix(path, "/engine/rendergraph/layout") {
			continue
		}
		pos := fset.PositionFor(imp.Pos(), false)
		diagnostics = append(diagnostics, Diagnostic{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
			Message:  "tui app userspace must not import engine/rendergraph/layout; compose view.ViewSpec only",
		})
	}
	return diagnostics
}

func tuiUserspaceLayoutAPIDiagnostics(fset *token.FileSet, file *ast.File) []Diagnostic {
	var diagnostics []Diagnostic
	ast.Inspect(file, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != "layout" {
			return true
		}
		switch sel.Sel.Name {
		case "FlowChild", "NewColumn", "NewRow", "NewResponsiveSwitch", "RenderNode":
			pos := fset.PositionFor(sel.Sel.Pos(), false)
			diagnostics = append(diagnostics, Diagnostic{
				Filename: pos.Filename,
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  "tui app userspace must not use raw layout API (layout.*); compose via view algebra",
			})
		}
		return true
	})
	return diagnostics
}

func tuiUserspaceBuildSignatureDiagnostics(fset *token.FileSet, file *ast.File) []Diagnostic {
	var diagnostics []Diagnostic
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type == nil || fn.Type.Results == nil {
			continue
		}
		if fn.Name == nil || fn.Name.Name != "BuildView" {
			continue
		}
		for _, result := range fn.Type.Results.List {
			sel, ok := result.Type.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "view" || sel.Sel == nil || sel.Sel.Name != "RenderNode" {
				continue
			}
			pos := fset.PositionFor(sel.Pos(), false)
			diagnostics = append(diagnostics, Diagnostic{
				Filename: pos.Filename,
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  "tui app userspace BuildView must return view.ViewSpec, not view.RenderNode",
			})
			continue
		}
	}
	return diagnostics
}

func cyclomaticComplexity(fn *ast.FuncDecl) int {
	complexity := 1
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if len(value.List) > 0 {
				complexity++
			}
		case *ast.CommClause:
			if value.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if value.Op == token.LAND || value.Op == token.LOR {
				complexity++
			}
		}
		return true
	})
	return complexity
}

func shouldSkipFile(filename string, file *ast.File) bool {
	if strings.HasSuffix(filename, "_test.go") {
		return true
	}
	if strings.Contains(filename, "/legacy/") {
		return true
	}
	if strings.Contains(filename, "/test/") {
		return true
	}
	if strings.Contains(filename, "/internal/devtools/") {
		return true
	}
	if strings.Contains(filename, "/testdata/") {
		return true
	}
	if strings.Contains(filename, "/.git/") {
		return true
	}
	if filepath.Base(filename) == "" {
		return true
	}
	for _, comment := range file.Comments {
		for _, line := range comment.List {
			if strings.Contains(line.Text, "Code generated") && strings.Contains(line.Text, "DO NOT EDIT") {
				return true
			}
		}
	}
	return false
}

func fileIgnoreSet(fset *token.FileSet, file *ast.File) map[string]bool {
	ignores := map[string]bool{}
	for _, comment := range file.Comments {
		if fset.PositionFor(comment.Pos(), false).Line > 5 {
			continue
		}
		for rule := range commentGroupIgnoreSet(comment) {
			ignores[rule] = true
		}
	}
	return ignores
}

func commentGroupIgnoreSet(group *ast.CommentGroup) map[string]bool {
	ignores := map[string]bool{}
	if group == nil {
		return ignores
	}
	for _, comment := range group.List {
		text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
		if !strings.HasPrefix(text, ignoreMarker) {
			continue
		}
		rule := strings.TrimSpace(strings.TrimPrefix(text, ignoreMarker))
		fields := strings.Fields(rule)
		if len(fields) == 0 {
			continue
		}
		rule = fields[0]
		if rule != "" {
			ignores[rule] = true
		}
	}
	return ignores
}
