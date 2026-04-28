package invariants

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var (
	camelTokenPattern   = regexp.MustCompile(`[A-Z]+[a-z0-9]*|[a-z0-9]+`)
	markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	bannedTopLevelDirs  = []string{
		"pkg",
		"common",
		"shared",
		"utils",
		"helpers",
		"lib",
		"src",
		"services",
		"controllers",
		"models",
		"entities",
		"ui",
	}
	bannedIdentifierTokens = []string{
		"plan",
		"agent",
		"manager",
		"service",
		"controller",
		"arrangement",
		"placeholder",
		"budget",
		"throttle",
		"receipt",
		"replay",
		"memory",
		"session",
		"capsule",
		"mesh",
		"fabric",
		"delegation",
		"gateway",
		"proxy",
		"router",
	}
	bannedImportPathParts = []string{
		"/legacy/",
		"/common",
		"/shared",
		"/utils",
		"/helpers",
		"/services",
		"/controllers",
		"/models",
		"/entities",
	}
	allowedIdentifierTokens = []string{
		"endpoint",
		"name",
		"target",
		"route",
		"policy",
		"retry",
		"provider",
		"binding",
		"compatibility",
		"profile",
		"capability",
		"family",
		"operation",
		"request",
		"attempt",
		"stream",
		"cancellation",
		"traffic",
		"event",
		"result",
		"class",
		"swobu",
		"backend",
		"error",
		"intent",
		"evidence",
	}
)

func TestModulePathEndsWithSwobu(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(repoPath(t, "swobucli", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	for _, line := range strings.Split(string(content), "\n") {
		if !strings.HasPrefix(line, "module ") {
			continue
		}

		modulePath := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		if !strings.HasSuffix(modulePath, "/swobu") {
			t.Fatalf("module path %q must end with /swobu", modulePath)
		}
		return
	}

	t.Fatal("module directive not found")
}

func TestForbiddenTopLevelDirectoriesAbsent(t *testing.T) {
	t.Parallel()

	for _, dir := range bannedTopLevelDirs {
		info, err := os.Stat(repoPath(t, dir))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if info.IsDir() {
			t.Fatalf("top-level directory %q is forbidden by repo structure law", dir)
		}
	}
}

func TestGreenfieldGoFilesAvoidBannedTermsAndImports(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	goFiles, err := collectGoFiles(root)
	if err != nil {
		t.Fatalf("collect go files: %v", err)
	}

	for _, path := range goFiles {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		if file.Name != nil && isBannedIdentifier(path, file.Name.Name) {
			t.Fatalf("%s declares forbidden package name %q", relativeToRoot(t, path), file.Name.Name)
		}

		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, part := range bannedImportPathParts {
				if strings.Contains(importPath, part) {
					t.Fatalf("%s imports forbidden path %q", relativeToRoot(t, path), importPath)
				}
			}
		}

		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.Ident:
				if isBannedIdentifier(path, n.Name) {
					t.Fatalf("%s uses forbidden identifier token %q", relativeToRoot(t, path), n.Name)
				}
			}
			return true
		})
	}
}

func TestCockpitVocabularyDoesNotDriftInAppUserspace(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	goFiles, err := collectGoFiles(root)
	if err != nil {
		t.Fatalf("collect go files: %v", err)
	}

	forbidden := []string{
		"selected target",
		"targets",
		"provider config",
		"credential source",
		"quick launch",
	}

	for _, path := range goFiles {
		rel := relativeToRoot(t, path)
		if !strings.HasPrefix(rel, "internal/adapters/inbound/tui/app/views/") {
			continue
		}
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := strings.ToLower(string(content))
		for _, phrase := range forbidden {
			if strings.Contains(text, phrase) {
				t.Fatalf("%s contains forbidden cockpit vocabulary phrase %q", rel, phrase)
			}
		}
	}
}

func TestAppSemanticRowsDoNotPassLayoutSkinStrings(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	goFiles, err := collectGoFiles(root)
	if err != nil {
		t.Fatalf("collect go files: %v", err)
	}

	for _, path := range goFiles {
		rel := relativeToRoot(t, path)
		if !strings.HasPrefix(rel, "internal/adapters/inbound/tui/app/views/") {
			continue
		}
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			_, sel := callBaseSelector(call.Fun)
			if sel != "ListItemRow" && sel != "ListItemRowWithHooks" && sel != "InsetLabel" {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}

			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			raw, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquote %s literal %q: %v", rel, lit.Value, err)
			}
			if raw != strings.TrimSpace(raw) {
				t.Fatalf("%s passes layout skin string %q into %s; semantic row constructors must not accept leading/trailing whitespace tokens", rel, raw, sel)
			}
			return true
		})
	}
}

func TestRepoSupportFilesPresent(t *testing.T) {
	t.Parallel()

	required := []string{
		"docs/README.md",
		"tasks/README.md",
		"tasks/templates/task-frame-template.md",
		"tasks/ready/README.md",
		"swobucli/scripts/create-task-from-template.sh",
		"swobucli/scripts/check-doc-policy.sh",
		"docs/examples/pty-harness.md",
	}

	for _, path := range required {
		info, err := os.Stat(repoPath(t, path))
		if err != nil {
			t.Fatalf("required repo-support file missing %q: %v", path, err)
		}
		if info.IsDir() {
			t.Fatalf("required repo-support path %q must be a file", path)
		}
	}
}

func TestCockpitModelCatalogNeverUsesFallbackModelLists(t *testing.T) {
	t.Parallel()

	path := repoPath(t, "swobucli", "internal", "adapters", "inbound", "tui", "app", "state", "effect", "effect_daemon.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read effect daemon file: %v", err)
	}
	text := string(content)
	forbidden := []string{
		"fallbackDraftModelIDs",
		"gpt-4.1-mini",
		"claude-3-7-sonnet",
		"llama3.1",
	}
	for _, token := range forbidden {
		if strings.Contains(text, token) {
			t.Fatalf("model catalog effect must fail fast and never invent model lists; found forbidden token %q", token)
		}
	}
}

func TestReadyTasksIncludeBuildVsBuySection(t *testing.T) {
	t.Parallel()

	taskFiles, err := filepath.Glob(repoPath(t, "tasks", "ready", "*.md"))
	if err != nil {
		t.Fatalf("glob ready tasks: %v", err)
	}

	for _, path := range taskFiles {
		if filepath.Base(path) == "README.md" {
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read task file %s: %v", relativeToRoot(t, path), err)
		}

		text := string(content)
		if !strings.Contains(text, "## Build vs Buy") {
			t.Fatalf("task %q missing Build vs Buy section", relativeToRoot(t, path))
		}
		required := []string{
			"- product-owned semantics:",
			"- commodity mechanics:",
			"- library candidates:",
			"- recommendation:",
			"- decision status:",
		}
		for _, field := range required {
			if !strings.Contains(text, field) {
				t.Fatalf("task %q missing Build vs Buy field %q", relativeToRoot(t, path), field)
			}
		}
		if strings.Contains(text, "- recommendation:\n") || strings.Contains(text, "- recommendation: \n") {
			t.Fatalf("task %q has empty Build vs Buy recommendation", relativeToRoot(t, path))
		}
		if strings.Contains(text, "- decision status: `decided` or `needs-user-choice`") {
			t.Fatalf("task %q still has template Build vs Buy decision status", relativeToRoot(t, path))
		}
	}
}

func TestReadyTasksIncludeDecisionEscalationSection(t *testing.T) {
	t.Parallel()

	taskFiles, err := filepath.Glob(repoPath(t, "tasks", "ready", "*.md"))
	if err != nil {
		t.Fatalf("glob ready tasks: %v", err)
	}

	for _, path := range taskFiles {
		if filepath.Base(path) == "README.md" {
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read task file %s: %v", relativeToRoot(t, path), err)
		}

		text := string(content)
		if !strings.Contains(text, "## Decision / Escalation") {
			t.Fatalf("task %q missing Decision / Escalation section", relativeToRoot(t, path))
		}

		required := []string{
			"- ambiguity level:",
			"- can Codex proceed autonomously:",
			"- must ask user explicitly:",
			"- tradeoff requiring escalation:",
			"- options if asking:",
		}
		for _, field := range required {
			if !strings.Contains(text, field) {
				t.Fatalf("task %q missing Decision / Escalation field %q", relativeToRoot(t, path), field)
			}
		}
	}
}

func TestSwobuSkillsHaveSkillMetadata(t *testing.T) {
	t.Parallel()

	skillBodies, err := filepath.Glob(repoPath(t, ".codex", "skills", "swobu-*", "SKILL.md"))
	if err != nil {
		t.Fatalf("glob skill bodies: %v", err)
	}
	if len(skillBodies) == 0 {
		t.Fatal("expected at least one swobu skill body")
	}

	for _, bodyPath := range skillBodies {
		dir := filepath.Dir(bodyPath)
		metadataPath := filepath.Join(dir, "agents", "openai.yaml")
		if _, err := os.Stat(metadataPath); err != nil {
			t.Fatalf("skill missing metadata %q: %v", relativeToRoot(t, metadataPath), err)
		}
	}
}

func TestDocsTreeHasExplicitReferences(t *testing.T) {
	t.Parallel()

	docFiles, err := collectDocsFiles(repoPath(t, "docs"))
	if err != nil {
		t.Fatalf("collect docs files: %v", err)
	}

	refFiles, err := collectReferenceFiles(repoRoot(t))
	if err != nil {
		t.Fatalf("collect reference files: %v", err)
	}

	for _, docPath := range docFiles {
		relativeDoc := relativeToRoot(t, docPath)
		docsRelative := strings.TrimPrefix(relativeDoc, "docs/")
		found := false

		for _, refPath := range refFiles {
			if refPath == docPath {
				continue
			}

			content, err := os.ReadFile(refPath)
			if err != nil {
				t.Fatalf("read reference file %s: %v", relativeToRoot(t, refPath), err)
			}

			text := string(content)
			if strings.Contains(text, relativeDoc) || strings.Contains(text, docsRelative) || markdownRefersToDoc(t, refPath, relativeDoc, text) {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("doc %q is not referenced from docs, skills, or project Makefiles", relativeDoc)
		}
	}
}

func markdownRefersToDoc(t *testing.T, refPath, relativeDoc, text string) bool {
	t.Helper()

	for _, match := range markdownLinkPattern.FindAllStringSubmatch(text, -1) {
		link := strings.TrimSpace(match[1])
		if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "mailto:") || strings.HasPrefix(link, "#") {
			continue
		}
		targetPart, _, _ := strings.Cut(link, "#")
		if targetPart == "" || strings.HasPrefix(targetPart, "/") {
			continue
		}
		target := filepath.Clean(filepath.Join(filepath.Dir(refPath), filepath.FromSlash(targetPart)))
		if relativeToRoot(t, target) == relativeDoc {
			return true
		}
	}

	return false
}

func collectGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		cleanPath := filepath.ToSlash(path)
		if d.IsDir() {
			switch filepath.Base(cleanPath) {
			case ".git", ".bin", ".codex", "legacy", "vendor":
				return filepath.SkipDir
			}
			return nil
		}

		if filepath.Ext(cleanPath) == ".go" {
			files = append(files, cleanPath)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(files)
	return files, nil
}

func collectDocsFiles(root string) ([]string, error) {
	var files []string
	skipDir := filepath.Join(root, "00-inbox")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path == skipDir {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".md" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(files)
	return files, nil
}

func collectReferenceFiles(root string) ([]string, error) {
	var files []string
	paths := []string{
		filepath.Join(root, "docs"),
		filepath.Join(root, ".codex", "skills"),
		filepath.Join(root, "swobucli", "Makefile"),
		filepath.Join(root, "swobucom", "Makefile"),
	}

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}

		if !info.IsDir() {
			files = append(files, path)
			continue
		}

		err = filepath.WalkDir(path, func(child string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			switch filepath.Ext(child) {
			case ".md", ".yaml", ".yml":
				files = append(files, child)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	slices.Sort(files)
	return files, nil
}

func isBannedIdentifier(path, name string) bool {
	if name == "" {
		return false
	}

	for _, token := range identifierTokens(name) {
		if token == "workspace" && allowsWorkspaceToken(path) {
			continue
		}
		if allowsBannedTokenInPath(path, token) {
			continue
		}
		if slices.Contains(allowedIdentifierTokens, token) {
			continue
		}
		if slices.Contains(bannedIdentifierTokens, token) {
			return true
		}
	}

	return false
}

func allowsWorkspaceToken(path string) bool {
	normalized := filepath.ToSlash(path)
	return strings.Contains(normalized, "/internal/adapters/inbound/tui/app/")
}

func allowsBannedTokenInPath(path, token string) bool {
	normalized := filepath.ToSlash(path)
	switch token {
	case "replay":
		return strings.Contains(normalized, "/test/") || strings.Contains(normalized, "/internal/devtools/livematrix/")
	case "session":
		return strings.Contains(normalized, "/internal/devtools/livematrix/") || strings.Contains(normalized, "/test/")
	case "router":
		return strings.Contains(normalized, "openrouter") || strings.Contains(normalized, "/test/")
	case "gateway":
		return strings.Contains(normalized, "/test/")
	default:
		return false
	}
}

func identifierTokens(name string) []string {
	normalized := strings.ReplaceAll(name, "-", "_")
	parts := strings.Split(normalized, "_")

	var tokens []string
	for _, part := range parts {
		if part == "" {
			continue
		}

		for _, token := range camelTokenPattern.FindAllString(part, -1) {
			tokens = append(tokens, strings.ToLower(token))
		}
	}

	return tokens
}

func repoPath(t *testing.T, parts ...string) string {
	t.Helper()

	allParts := append([]string{repoRoot(t)}, parts...)
	return filepath.Join(allParts...)
}

func relativeToRoot(t *testing.T, path string) string {
	t.Helper()

	relativePath, err := filepath.Rel(repoRoot(t), path)
	if err != nil {
		t.Fatalf("make path relative: %v", err)
	}

	return filepath.ToSlash(relativePath)
}

func callBaseSelector(expr ast.Expr) (pkgName string, selector string) {
	switch v := expr.(type) {
	case *ast.SelectorExpr:
		if ident, ok := v.X.(*ast.Ident); ok {
			return ident.Name, v.Sel.Name
		}
		return "", v.Sel.Name
	case *ast.IndexExpr:
		return callBaseSelector(v.X)
	case *ast.IndexListExpr:
		return callBaseSelector(v.X)
	default:
		return "", ""
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve working directory: %v", err)
	}
	dir := wd
	for {
		if fileExists(filepath.Join(dir, "AGENTS.md")) && dirExists(filepath.Join(dir, "swobucli")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("resolve repo root: could not locate AGENTS.md + swobucli from %s", wd)
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func TestEngineElementDoesNotLeakOutsideEngine(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	goFiles, err := collectGoFiles(root)
	if err != nil {
		t.Fatalf("collect go files: %v", err)
	}

	bannedEngineTypes := []string{
		"layout.Element",
		"layout.Composite",
		"layout.LayoutNode",
		"layout.PaintContext",
	}

	for _, path := range goFiles {
		rel := relativeToRoot(t, path)

		// Toolkit legitimately uses engine structural types — it's the bridge layer.
		// Only app code is forbidden from returning them; app must use view.ViewSpec[M].
		if !strings.Contains(rel, "internal/adapters/inbound/tui/app/") {
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		// Check function return types.
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type.Results == nil {
				continue
			}
			// Test functions are allowed to return anything.
			if strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			// BuildView is the ViewBuilder contract — returning layout.Element from BuildView is correct.
			if fn.Name.Name == "BuildView" {
				continue
			}
			// Private helper functions (lowercase) are allowed — they're internal to the package.
			if fn.Name.Name != "" && fn.Name.Name[0] >= 'a' && fn.Name.Name[0] <= 'z' {
				continue
			}
			for _, field := range fn.Type.Results.List {
				if se, ok := field.Type.(*ast.SelectorExpr); ok {
					if ident, ok := se.X.(*ast.Ident); ok {
						ref := ident.Name + "." + se.Sel.Name
						for _, banned := range bannedEngineTypes {
							if ref == banned {
								t.Fatalf("%s returns forbidden engine structural type %q from %s; use view.ViewSpec[M] instead", rel, banned, fn.Name.Name)
							}
						}
					}
				}
			}
		}
	}
}

// bannedEngineImportPaths are forbidden from app code. These are engine internals
// that should only be visible to engine and toolkit packages.
var bannedEngineImportPaths = []string{

	"engine/host",
}

func TestEngineInternalsNotImportedByAppCode(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	goFiles, err := collectGoFiles(root)
	if err != nil {
		t.Fatalf("collect go files: %v", err)
	}

	for _, path := range goFiles {
		rel := relativeToRoot(t, path)

		// App userspace code must not import engine internals.
		if !strings.Contains(rel, "internal/adapters/inbound/tui/app/views/") {
			continue
		}
		// Test files are allowed to import engine internals for verification.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, banned := range bannedEngineImportPaths {
				if strings.Contains(importPath, banned) {
					t.Fatalf("%s imports forbidden engine internal %q; app code must use view.ViewSpec[M] composition instead of engine internals", rel, banned)
				}
			}
		}
	}
}
