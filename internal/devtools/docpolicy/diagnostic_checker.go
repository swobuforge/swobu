// single checker file because the graph rules are tightly coupled.
package docpolicy

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/metrofun/swobu/internal/devtools/executionledger"
)

const (
	softCap        = 12 * 1024
	hardCap        = 16 * 1024
	parentIndexCap = 4 * 1024
)

var (
	linkRE       = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	plainDocRE   = regexp.MustCompile(`docs/[A-Za-z0-9./_-]+\.md`)
	headingRE    = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	fencedCodeRE = regexp.MustCompile("(?s)```.*?```")

	rootEntrypoints = map[string]struct{}{
		"docs/README.md": {},
	}
	nonNormativeFiles = map[string]struct{}{
		"docs/README.md":             {},
		"docs/working-agreements.md": {},
	}
	nonNormativePrefixes = []string{
		"docs/examples/",
	}
	searchRoots = []string{
		"docs",
		"tasks",
		".codex/skills",
		"swobucli/scripts",
		"AGENTS.md",
		"swobucli/Makefile",
		"swobucom/Makefile",
	}
	rootInstructionMarkers = []string{
		"# swobu agent instructions",
		"onboarding entrypoint",
		"before implementing",
		"required task frame",
		"source of truth",
		"root instruction surface",
	}
)

type Diagnostic struct {
	Filename string
	Message  string
	Warning  bool
}

// coupled policy passes over one rooted documentation tree.
func Check(root string) ([]Diagnostic, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	docsDir := filepath.Join(root, "docs")
	mdFiles, err := markdownFiles(docsDir)
	if err != nil {
		return nil, err
	}

	var diagnostics []Diagnostic
	diagnostics = append(diagnostics, checkRootInstructionSurfaces(root)...)
	diagnostics = append(diagnostics, checkSizes(root, mdFiles)...)
	refs, err := referencedDocs(root)
	if err != nil {
		return nil, err
	}
	diagnostics = append(diagnostics, checkReachability(root, mdFiles, refs)...)
	diagnostics = append(diagnostics, checkLinks(root, mdFiles)...)
	diagnostics = append(diagnostics, checkEntrypointFanout(root)...)
	diagnostics = append(diagnostics, checkDocsReadmeHierarchy(root)...)
	diagnostics = append(diagnostics, checkTaskGoverningDocPointers(root)...)
	diagnostics = append(diagnostics, checkTaskBuildVsBuyEvidence(root)...)
	diagnostics = append(diagnostics, checkExecutionLedger(root)...)
	diagnostics = append(diagnostics, checkReleaseNoteDiscipline(root)...)
	diagnostics = append(diagnostics, checkTelemetryReleaseDiscipline(root)...)

	slices.SortFunc(diagnostics, func(a, b Diagnostic) int {
		if a.Warning != b.Warning {
			if a.Warning {
				return -1
			}
			return 1
		}
		if a.Filename != b.Filename {
			return strings.Compare(a.Filename, b.Filename)
		}
		return strings.Compare(a.Message, b.Message)
	})
	return diagnostics, nil
}

func checkReleaseNoteDiscipline(root string) []Diagnostic {
	rolloutPath := filepath.Join(
		root,
		"docs",
		"05-engineering",
		"release-versioning-and-migration",
		"release-gates-rollout-and-rollback.md",
	)
	breakingPath := filepath.Join(
		root,
		"docs",
		"05-engineering",
		"release-versioning-and-migration",
		"breaking-changes-and-support-bands.md",
	)

	var diagnostics []Diagnostic
	rolloutText, ok := safeText(rolloutPath)
	if !ok {
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, rolloutPath),
			Message:  "could not read release-gates doc for release-note discipline check",
		})
		return diagnostics
	}
	for _, required := range []string{
		"## Release note standard",
		"Required sections:",
		"Support statement",
		"| **Release note** |",
	} {
		if strings.Contains(rolloutText, required) {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, rolloutPath),
			Message:  fmt.Sprintf("release-note discipline missing required marker %q", required),
		})
	}

	breakingText, ok := safeText(breakingPath)
	if !ok {
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, breakingPath),
			Message:  "could not read support-band doc for release-note discipline check",
		})
		return diagnostics
	}
	for _, required := range []string{
		"## 8.2 Release-note rule",
		"## 8.3 Support claim rule",
	} {
		if strings.Contains(breakingText, required) {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, breakingPath),
			Message:  fmt.Sprintf("support-band release-note discipline missing required marker %q", required),
		})
	}
	return diagnostics
}

func checkTelemetryReleaseDiscipline(root string) []Diagnostic {
	rolloutPath := filepath.Join(
		root,
		"docs",
		"05-engineering",
		"release-versioning-and-migration",
		"release-gates-rollout-and-rollback.md",
	)
	telemetryPath := filepath.Join(
		root,
		"docs",
		"05-engineering",
		"observability-and-operability",
		"product-telemetry-v1.md",
	)

	var diagnostics []Diagnostic
	rolloutText, ok := safeText(rolloutPath)
	if !ok {
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, rolloutPath),
			Message:  "could not read release-gates doc for telemetry release discipline check",
		})
		return diagnostics
	}
	for _, required := range []string{
		"Telemetry disclosure",
		"opt-out",
		"`swobu telemetry off`",
		"`swobu telemetry status`",
	} {
		if strings.Contains(rolloutText, required) {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, rolloutPath),
			Message:  fmt.Sprintf("telemetry release discipline missing required marker %q", required),
		})
	}

	telemetryText, ok := safeText(telemetryPath)
	if !ok {
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, telemetryPath),
			Message:  "could not read product-telemetry-v1 doc for telemetry release discipline check",
		})
		return diagnostics
	}
	for _, required := range []string{
		"single canonical specification",
		"enabled by default with user opt-out",
		"Release blockers for telemetry-enabled launch",
	} {
		if strings.Contains(telemetryText, required) {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, telemetryPath),
			Message:  fmt.Sprintf("telemetry contract doc missing required marker %q", required),
		})
	}
	return diagnostics
}

func checkRootInstructionSurfaces(root string) []Diagnostic {
	var diagnostics []Diagnostic
	if err := ensureFileExists(filepath.Join(root, "AGENTS.md")); err != nil {
		return []Diagnostic{{
			Filename: "AGENTS.md",
			Message:  err.Error(),
		}}
	}

	rootMarkdownFiles, err := filepath.Glob(filepath.Join(root, "*.md"))
	if err != nil {
		return []Diagnostic{{
			Filename: ".",
			Message:  fmt.Sprintf("could not enumerate root markdown files: %v", err),
		}}
	}
	for _, path := range rootMarkdownFiles {
		if filepath.Base(path) == "AGENTS.md" {
			continue
		}
		text, ok := safeText(path)
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: rel(root, path),
				Message:  "could not read root markdown file for instruction-entrypoint check",
			})
			continue
		}
		lower := strings.ToLower(text)
		for _, marker := range rootInstructionMarkers {
			if strings.Contains(lower, marker) {
				diagnostics = append(diagnostics, Diagnostic{
					Filename: rel(root, path),
					Message:  "root markdown file competes with AGENTS.md as an instruction entrypoint",
				})
				break
			}
		}
	}
	return diagnostics
}

func checkEntrypointFanout(root string) []Diagnostic {
	paths := []string{
		filepath.Join(root, "docs", "README.md"),
		filepath.Join(root, "docs", "01-product", "README.md"),
		filepath.Join(root, "docs", "02-domain", "README.md"),
		filepath.Join(root, "docs", "03-architecture", "README.md"),
		filepath.Join(root, "docs", "04-design", "README.md"),
		filepath.Join(root, "docs", "05-engineering", "README.md"),
		filepath.Join(root, "docs", "07-governance", "README.md"),
		filepath.Join(root, "tasks", "README.md"),
	}
	var diagnostics []Diagnostic
	for _, path := range paths {
		diagnostics = append(diagnostics, checkMarkdownSectionFanout(root, path, 9)...)
	}
	return diagnostics
}

func checkMarkdownSectionFanout(root, path string, maxChildren int) []Diagnostic {
	text, ok := safeText(path)
	if !ok {
		return []Diagnostic{{
			Filename: rel(root, path),
			Message:  "could not read markdown entrypoint for fanout check",
		}}
	}

	var diagnostics []Diagnostic
	lines := strings.Split(text, "\n")
	currentHeading := ""
	currentCount := 0
	flush := func() {
		if currentHeading == "" {
			return
		}
		if currentCount > maxChildren {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: rel(root, path),
				Message: fmt.Sprintf(
					"entrypoint section %q exceeds fanout limit (%d > %d)",
					currentHeading,
					currentCount,
					maxChildren,
				),
			})
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			currentHeading = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			currentCount = 0
			continue
		}
		if currentHeading == "" {
			continue
		}
		if strings.HasPrefix(line, "- [") {
			currentCount++
		}
	}
	flush()
	return diagnostics
}

func checkDocsReadmeHierarchy(root string) []Diagnostic {
	path := filepath.Join(root, "docs", "README.md")
	text, ok := safeText(path)
	if !ok {
		return []Diagnostic{{
			Filename: rel(root, path),
			Message:  "could not read docs/README.md for hierarchy check",
		}}
	}

	allowed := map[string]struct{}{
		"01-product/README.md":      {},
		"02-domain/README.md":       {},
		"03-architecture/README.md": {},
		"04-design/README.md":       {},
		"05-engineering/README.md":  {},
		"07-governance/README.md":   {},
		"examples/pty-harness.md":   {},
		"working-agreements.md":     {},
	}

	var diagnostics []Diagnostic
	for _, link := range markdownLinks(text) {
		if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "mailto:") || strings.HasPrefix(link, "#") {
			continue
		}
		targetPart, _, _ := strings.Cut(link, "#")
		target := filepath.ToSlash(filepath.Clean(targetPart))
		if _, ok := allowed[target]; ok {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, path),
			Message:  fmt.Sprintf("root docs README must route through section README hierarchy, found direct link to %q", target),
		})
	}
	return diagnostics
}

func checkTaskGoverningDocPointers(root string) []Diagnostic {
	taskRoots := []string{
		filepath.Join(root, "tasks", "ready", "04-runtime-cli"),
		filepath.Join(root, "tasks", "ready", "05-tui"),
	}

	var diagnostics []Diagnostic
	for _, taskRoot := range taskRoots {
		err := filepath.WalkDir(taskRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			text, ok := safeText(path)
			if !ok {
				diagnostics = append(diagnostics, Diagnostic{
					Filename: rel(root, path),
					Message:  "could not read task file for governing-doc quality check",
				})
				return nil
			}
			for _, doc := range governingDocsFromTask(text) {
				if !isDecomposedParentDoc(root, doc) {
					continue
				}
				diagnostics = append(diagnostics, Diagnostic{
					Filename: rel(root, path),
					Message:  fmt.Sprintf("governing docs cite decomposed parent index %q; replace it with the exact child doc", doc),
					Warning:  true,
				})
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: rel(root, taskRoot),
				Message:  fmt.Sprintf("could not walk task lane for governing-doc quality check: %v", err),
			})
		}
	}
	return diagnostics
}

func checkTaskBuildVsBuyEvidence(root string) []Diagnostic {
	taskRoot := filepath.Join(root, "tasks", "ready")
	var diagnostics []Diagnostic
	err := filepath.WalkDir(taskRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "README.md" {
			return nil
		}
		text, ok := safeText(path)
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: rel(root, path),
				Message:  "could not read task file for build-vs-buy evidence check",
			})
			return nil
		}
		section, found := markdownSection(text, "Build vs Buy")
		if !found {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: rel(root, path),
				Message:  "task frame must include a Build vs Buy section",
			})
			return nil
		}
		required := []string{
			"- product-owned semantics:",
			"- commodity mechanics:",
			"- library candidates:",
			"- recommendation:",
			"- decision status:",
		}
		for _, marker := range required {
			if !strings.Contains(section, marker) {
				diagnostics = append(diagnostics, Diagnostic{
					Filename: rel(root, path),
					Message:  fmt.Sprintf("Build vs Buy section missing required line %q", marker),
				})
			}
		}
		status := buildVsBuyDecisionStatus(section)
		if status == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: rel(root, path),
				Message:  "Build vs Buy section must set decision status to `decided` or `needs-user-choice`",
			})
			return nil
		}
		if status != "`decided`" && status != "`needs-user-choice`" {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: rel(root, path),
				Message:  fmt.Sprintf("Build vs Buy decision status %q is invalid; use `decided` or `needs-user-choice`", status),
			})
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		diagnostics = append(diagnostics, Diagnostic{
			Filename: rel(root, taskRoot),
			Message:  fmt.Sprintf("could not walk task lane for build-vs-buy evidence check: %v", err),
		})
	}
	return diagnostics
}

func checkExecutionLedger(root string) []Diagnostic {
	ledgerDiagnostics, err := executionledger.Check(root)
	if err != nil {
		return []Diagnostic{{
			Filename: "tasks/ready/00-reimplementation-backlog.md",
			Message:  fmt.Sprintf("execution ledger check failed: %v", err),
		}}
	}
	diagnostics := make([]Diagnostic, 0, len(ledgerDiagnostics))
	for _, diagnostic := range ledgerDiagnostics {
		diagnostics = append(diagnostics, Diagnostic{
			Filename: diagnostic.Filename,
			Message:  diagnostic.Message,
		})
	}
	return diagnostics
}

func markdownSection(text, sectionName string) (string, bool) {
	lines := strings.Split(text, "\n")
	var section []string
	inSection := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			inSection = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == sectionName
			continue
		}
		if inSection {
			section = append(section, line)
		}
	}
	if !inSection {
		return "", false
	}
	return strings.Join(section, "\n"), true
}

func buildVsBuyDecisionStatus(section string) string {
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- decision status:") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "- decision status:"))
	}
	return ""
}

func governingDocsFromTask(text string) []string {
	lines := strings.Split(text, "\n")
	var section []string
	inSection := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			inSection = strings.TrimSpace(line) == "## Governing Docs"
			continue
		}
		if inSection {
			section = append(section, line)
		}
	}
	joined := strings.Join(section, "\n")
	matches := plainDocRE.FindAllString(joined, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var docs []string
	for _, match := range matches {
		clean := filepath.ToSlash(filepath.Clean(match))
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		docs = append(docs, clean)
	}
	return docs
}

func isDecomposedParentDoc(root, relPath string) bool {
	if filepath.Ext(relPath) != ".md" {
		return false
	}
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return false
	}
	dir := strings.TrimSuffix(abs, ".md")
	childEntries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range childEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		return true
	}
	return false
}

func markdownFiles(root string) ([]string, error) {
	var files []string
	skipDir := filepath.Join(root, "00-inbox")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
	return files, err
}

func checkSizes(root string, files []string) []Diagnostic {
	var diagnostics []Diagnostic
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: rel(root, file),
				Message:  fmt.Sprintf("stat failed: %v", err),
			})
			continue
		}
		relFile := rel(root, file)
		size := info.Size()
		if isParentIndex(file) && size > parentIndexCap {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: relFile,
				Message:  fmt.Sprintf("parent index too large (%d bytes > %d)", size, parentIndexCap),
			})
		}
		if !isNormative(relFile) {
			continue
		}
		switch {
		case size > hardCap:
			diagnostics = append(diagnostics, Diagnostic{
				Filename: relFile,
				Message:  fmt.Sprintf("normative doc over hard cap (%d bytes > %d)", size, hardCap),
			})
		case size > softCap:
			diagnostics = append(diagnostics, Diagnostic{
				Filename: relFile,
				Message:  fmt.Sprintf("normative doc over soft cap (%d bytes > %d)", size, softCap),
				Warning:  true,
			})
		}
	}
	return diagnostics
}

func referencedDocs(root string) (map[string]map[string]struct{}, error) {
	refs := map[string]map[string]struct{}{}
	for _, rootEntry := range searchRoots {
		path := filepath.Join(root, rootEntry)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if info.IsDir() {
			err = filepath.WalkDir(path, func(current string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				readAndCollectRefs(root, current, refs)
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		readAndCollectRefs(root, path, refs)
	}
	return refs, nil
}

// code fences, and normalized relative targets in one traversal.
func readAndCollectRefs(root, source string, refs map[string]map[string]struct{}) {
	text, ok := safeText(source)
	if !ok {
		return
	}
	sourceRel := rel(root, source)
	for _, link := range markdownLinks(text) {
		if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "mailto:") || strings.HasPrefix(link, "#") {
			continue
		}
		targetPart, _, _ := strings.Cut(link, "#")
		if targetPart == "" || strings.HasPrefix(targetPart, "/") {
			continue
		}
		target := filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(targetPart)))
		if !strings.HasPrefix(target, root) {
			continue
		}
		if filepath.Ext(target) == ".md" {
			if _, err := os.Stat(target); err == nil {
				addRef(refs, rel(root, target), sourceRel)
			}
		}
	}
	for _, match := range plainDocRE.FindAllString(stripFencedCode(text), -1) {
		addRef(refs, filepath.ToSlash(filepath.Clean(match)), sourceRel)
	}
}

func addRef(refs map[string]map[string]struct{}, target, source string) {
	if refs[target] == nil {
		refs[target] = map[string]struct{}{}
	}
	refs[target][source] = struct{}{}
}

func checkReachability(root string, files []string, refs map[string]map[string]struct{}) []Diagnostic {
	var diagnostics []Diagnostic
	for _, file := range files {
		relFile := rel(root, file)
		if _, ok := rootEntrypoints[relFile]; ok {
			continue
		}
		sources := refs[relFile]
		count := 0
		for source := range sources {
			if source != relFile {
				count++
			}
		}
		if count == 0 {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: relFile,
				Message:  "unreferenced doc",
			})
		}
	}
	return diagnostics
}

// anchors, absolute targets, and missing local docs in one pass.
func checkLinks(root string, files []string) []Diagnostic {
	var diagnostics []Diagnostic
	anchorCache := map[string]map[string]struct{}{}
	for _, file := range files {
		text, _ := os.ReadFile(file)
		for _, link := range markdownLinks(string(text)) {
			if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "mailto:") {
				continue
			}
			relFile := rel(root, file)
			if strings.HasPrefix(link, "#") {
				anchors := anchorsFor(file, anchorCache)
				if _, ok := anchors[link[1:]]; !ok {
					diagnostics = append(diagnostics, Diagnostic{
						Filename: relFile,
						Message:  fmt.Sprintf("broken anchor %q", link),
					})
				}
				continue
			}
			targetPart, anchor, _ := strings.Cut(link, "#")
			target := filepath.Clean(filepath.Join(filepath.Dir(file), filepath.FromSlash(targetPart)))
			if !strings.HasPrefix(target, root) {
				diagnostics = append(diagnostics, Diagnostic{
					Filename: relFile,
					Message:  fmt.Sprintf("link escapes repo: %s", link),
				})
				continue
			}
			if _, err := os.Stat(target); err != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Filename: relFile,
					Message:  fmt.Sprintf("missing link target: %s", link),
				})
				continue
			}
			if anchor == "" || filepath.Ext(target) != ".md" {
				continue
			}
			anchors := anchorsFor(target, anchorCache)
			if _, ok := anchors[anchor]; !ok {
				diagnostics = append(diagnostics, Diagnostic{
					Filename: relFile,
					Message:  fmt.Sprintf("broken target anchor: %s", link),
				})
			}
		}
	}
	return diagnostics
}

func anchorsFor(path string, cache map[string]map[string]struct{}) map[string]struct{} {
	if anchors, ok := cache[path]; ok {
		return anchors
	}
	text, _ := os.ReadFile(path)
	anchors := map[string]struct{}{}
	for _, line := range strings.Split(string(text), "\n") {
		m := headingRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		anchors[slugify(m[2])] = struct{}{}
	}
	cache[path] = anchors
	return anchors
}

func markdownLinks(text string) []string {
	text = stripFencedCode(text)
	matches := linkRE.FindAllStringSubmatch(text, -1)
	links := make([]string, 0, len(matches))
	for _, match := range matches {
		links = append(links, strings.TrimSpace(match[1]))
	}
	return links
}

func stripFencedCode(text string) string {
	return fencedCodeRE.ReplaceAllString(text, "")
}

func safeText(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return "", false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	return string(data), true
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "`", "")
	var b strings.Builder
	lastHyphen := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastHyphen = false
		case unicode.IsSpace(r) || r == '-':
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func isNormative(relPath string) bool {
	if _, ok := nonNormativeFiles[relPath]; ok {
		return false
	}
	for _, prefix := range nonNormativePrefixes {
		if strings.HasPrefix(relPath, prefix) {
			return false
		}
	}
	return true
}

func isParentIndex(path string) bool {
	stem := strings.TrimSuffix(path, filepath.Ext(path))
	info, err := os.Stat(stem)
	return err == nil && info.IsDir()
}

func rel(root, target string) string {
	rp, err := filepath.Rel(root, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rp)
}

func ensureFileExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("required root instruction file missing")
		}
		return fmt.Errorf("could not stat required root instruction file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("required root instruction file is a directory")
	}
	return nil
}
