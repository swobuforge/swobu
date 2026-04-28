package invariants

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const maxHumanScaleFanout = 9

var maxHumanScaleFanoutOverrides = map[string]int{
	"tasks/ready/03-request-path/slices/model-selection": 12,
	"tasks/ready/05-tui/slices/foundation":               14,
	"tasks/ready/06-proof-release":                       30,
	"tasks/ready/07-execution-system":                    16,
	"swobucli/scripts":                                   10,
}

func TestRepoScriptsHaveUpstreamReferences(t *testing.T) {
	t.Parallel()

	scriptFiles, err := filepath.Glob(repoPath(t, "swobucli", "scripts", "*"))
	if err != nil {
		t.Fatalf("glob scripts: %v", err)
	}

	refFiles, err := collectEntrypointReferenceFiles(repoRoot(t))
	if err != nil {
		t.Fatalf("collect entrypoint reference files: %v", err)
	}

	for _, scriptPath := range scriptFiles {
		info, err := os.Stat(scriptPath)
		if err != nil {
			t.Fatalf("stat script %s: %v", relativeToRoot(t, scriptPath), err)
		}
		if info.IsDir() {
			continue
		}
		if filepath.Ext(scriptPath) != ".sh" {
			continue
		}

		relativeScript := relativeToRoot(t, scriptPath)
		if !artifactReferenced(refFiles, nil, relativeScript, relativeScript) {
			t.Fatalf("script %q is not referenced by a durable repo entrypoint", relativeScript)
		}
	}
}

func TestRepoOwnedSkillsHaveUpstreamReferences(t *testing.T) {
	t.Parallel()

	skillBodies, err := filepath.Glob(repoPath(t, ".codex", "skills", "swobu-*", "SKILL.md"))
	if err != nil {
		t.Fatalf("glob skill bodies: %v", err)
	}

	refFiles, err := collectEntrypointReferenceFiles(repoRoot(t))
	if err != nil {
		t.Fatalf("collect entrypoint reference files: %v", err)
	}

	for _, skillPath := range skillBodies {
		skillName := filepath.Base(filepath.Dir(skillPath))
		if !artifactReferenced(refFiles, nil, skillPath, skillName, relativeToRoot(t, skillPath)) {
			t.Fatalf("skill %q is not referenced by a durable repo entrypoint", skillName)
		}
	}
}

func TestPublicMakeTargetsHaveUpstreamReferences(t *testing.T) {
	t.Parallel()

	refFiles, err := collectEntrypointReferenceFiles(repoRoot(t))
	if err != nil {
		t.Fatalf("collect entrypoint reference files: %v", err)
	}

	projectMakefiles := []string{
		repoPath(t, "swobucli", "Makefile"),
		repoPath(t, "swobucom", "Makefile"),
	}

	for _, makefilePath := range projectMakefiles {
		targets, err := collectPublicMakeTargets(makefilePath)
		if err != nil {
			t.Fatalf("collect public make targets from %s: %v", relativeToRoot(t, makefilePath), err)
		}
		for _, target := range targets {
			if !makeTargetReferenced(makefilePath, refFiles, target) {
				t.Fatalf("make target %q in %s is not referenced by another make target, hook, skill, task, or doc", target, relativeToRoot(t, makefilePath))
			}
		}
	}
}

func TestRootMakefileIsAbsent(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat(repoPath(t, "Makefile")); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			t.Fatal("root Makefile must remain absent; use project-scoped make entrypoints only")
		}
		t.Fatalf("stat root Makefile: %v", err)
	}
}

func TestDeveloperFacingDirectoriesStayHumanScale(t *testing.T) {
	t.Parallel()

	roots := []string{
		repoPath(t, "docs"),
		repoPath(t, "tasks"),
		repoPath(t, "swobucli", "scripts"),
		repoPath(t, ".githooks"),
		repoPath(t, ".codex", "skills"),
		repoPath(t, ".github"),
	}

	// Skip directories that are .gitignore'd (inbox is a scratch area).
	skipDir := map[string]bool{
		repoPath(t, "docs", "00-inbox"): true,
	}

	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && skipDir[path] {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				return nil
			}

			entries, err := os.ReadDir(path)
			if err != nil {
				return err
			}

			fileCount := 0
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				fileCount++
			}

			relativePath := relativeToRoot(t, path)
			allowedFanout := maxHumanScaleFanout
			if override, ok := maxHumanScaleFanoutOverrides[relativePath]; ok {
				allowedFanout = override
			}
			if fileCount > allowedFanout {
				return &fanoutError{
					path:       relativePath,
					fileCount:  fileCount,
					maxAllowed: allowedFanout,
				}
			}
			return nil
		})
		if err != nil {
			var fanout *fanoutError
			if ok := asFanoutError(err, &fanout); ok {
				t.Fatalf("%s has %d direct files; max allowed is %d", fanout.path, fanout.fileCount, fanout.maxAllowed)
			}
			t.Fatalf("walk %s: %v", relativeToRoot(t, root), err)
		}
	}
}

func TestTaskGroupsHaveReadme(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(repoPath(t, "tasks", "ready"))
	if err != nil {
		t.Fatalf("read tasks/ready: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		readmePath := repoPath(t, "tasks", "ready", entry.Name(), "README.md")
		info, err := os.Stat(readmePath)
		if err != nil {
			t.Fatalf("task group %q missing README.md: %v", entry.Name(), err)
		}
		if info.IsDir() {
			t.Fatalf("task group %q README path is a directory", entry.Name())
		}
	}
}

func TestLegacyCompatibilityTestLanesAreAbsent(t *testing.T) {
	t.Parallel()

	legacyDirs := []string{
		repoPath(t, "test", "contract"),
		repoPath(t, "test", "conformance"),
	}
	for _, path := range legacyDirs {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("stat %s: %v", relativeToRoot(t, path), err)
		}
		if info.IsDir() {
			t.Fatalf("legacy compatibility lane %q must not exist; use test/compatibility/{surface,runtime}", relativeToRoot(t, path))
		}
	}
}

func collectEntrypointReferenceFiles(root string) ([]string, error) {
	var files []string
	paths := []string{
		filepath.Join(root, "docs"),
		filepath.Join(root, "tasks"),
		filepath.Join(root, ".codex", "skills"),
		filepath.Join(root, ".githooks"),
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
			case ".md", ".yaml", ".yml", ".sh":
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

func artifactReferenced(refFiles []string, skip map[string]struct{}, self string, needles ...string) bool {
	for _, refPath := range refFiles {
		if skip != nil {
			if _, ok := skip[refPath]; ok {
				continue
			}
		}
		if self != "" && refPath == self {
			continue
		}

		content, err := os.ReadFile(refPath)
		if err != nil {
			return false
		}
		text := string(content)
		for _, needle := range needles {
			if needle != "" && strings.Contains(text, needle) {
				return true
			}
		}
	}

	return false
}

func collectPublicMakeTargets(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var targets []string
	lines := strings.Split(string(content), "\n")
	inPhony := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inPhony && !strings.HasPrefix(trimmed, ".PHONY:") {
			continue
		}

		if strings.HasPrefix(trimmed, ".PHONY:") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, ".PHONY:"))
		}

		hasContinuation := strings.HasSuffix(trimmed, "\\")
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "\\"))
		if trimmed != "" {
			targets = append(targets, strings.Fields(trimmed)...)
		}

		inPhony = hasContinuation
	}

	slices.Sort(targets)
	return targets, nil
}

func makeTargetReferenced(makefilePath string, refFiles []string, target string) bool {
	makeRefPattern := regexp.MustCompile(`(^|[^A-Za-z0-9_-])make[[:space:]]+` + regexp.QuoteMeta(target) + `([^A-Za-z0-9_-]|$)`)
	makeTokenPattern := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(target) + `([^A-Za-z0-9_-]|$)`)

	for _, refPath := range refFiles {
		content, err := os.ReadFile(refPath)
		if err != nil {
			return false
		}

		if refPath != makefilePath {
			if makeRefPattern.Match(content) {
				return true
			}
			continue
		}

		lines := strings.Split(string(content), "\n")
		inPhony := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)

			if inPhony || strings.HasPrefix(trimmed, ".PHONY:") {
				inPhony = strings.HasSuffix(trimmed, "\\")
				continue
			}
			if strings.HasPrefix(trimmed, target+":") {
				continue
			}
			if makeTokenPattern.MatchString(line) {
				return true
			}
		}
	}

	return false
}

type fanoutError struct {
	path       string
	fileCount  int
	maxAllowed int
}

func (e *fanoutError) Error() string {
	return e.path + ":" + strconv.Itoa(e.fileCount)
}

func asFanoutError(err error, target **fanoutError) bool {
	return errors.As(err, target)
}
