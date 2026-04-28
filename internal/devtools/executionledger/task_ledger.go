package executionledger

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type BacklogStatus string

const (
	BacklogStatusDone    BacklogStatus = "done"
	BacklogStatusPending BacklogStatus = "pending"
	BacklogStatusOther   BacklogStatus = "other"
)

type Verdict string

const (
	VerdictNotStarted  Verdict = "not_started"
	VerdictInProgress  Verdict = "in_progress"
	VerdictImplemented Verdict = "implemented"
	VerdictProven      Verdict = "proven"
	VerdictDoneScoped  Verdict = "done_scoped"
)

type ClaimStatus string

const (
	ClaimStatusNotStarted  ClaimStatus = "not_started"
	ClaimStatusInProgress  ClaimStatus = "in_progress"
	ClaimStatusImplemented ClaimStatus = "implemented"
	ClaimStatusProven      ClaimStatus = "proven"
)

type Claim struct {
	Checked   bool
	Claim     string
	Proof     string
	Status    ClaimStatus
	Validated string
}

type Task struct {
	Path          string
	BacklogStatus BacklogStatus
	Tags          []string
	Verdict       Verdict
	DoneScope     string
	NotDoneScope  string
	Claims        []Claim
}

type Diagnostic struct {
	Filename string
	Message  string
}

var (
	backtickTaskRE = regexp.MustCompile("`([^`]+\\.md)`")
	claimLineRE    = regexp.MustCompile(`^-\s+\[( |x|X)\]\s+claim:\s*(.+?)\s*\|\s*proof:\s*` + "`?" + `(.+?)` + "`?" + `\s*\|\s*status:\s*` + "`?" + `(not_started|in_progress|implemented|proven)` + "`?" + `\s*\|\s*validated:\s*` + "`?" + `(\d{4}-\d{2}-\d{2})` + "`?" + `\s*$`)
)

func LoadP0Tasks(root string) ([]Task, error) {
	return LoadBacklogTasks(root, TaskFilter{
		RequireAnyTag: []string{"P0"},
	})
}

type TaskFilter struct {
	RequireAnyTag []string
	PathPrefix    string
}

func LoadBacklogTasks(root string, filter TaskFilter) ([]Task, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	backlogPath := filepath.Join(root, "tasks", "ready", "00-reimplementation-backlog.md")
	backlogText, err := os.ReadFile(backlogPath)
	if err != nil {
		return nil, fmt.Errorf("read backlog: %w", err)
	}
	entries := backlogEntries(string(backlogText))
	if len(entries) == 0 {
		return nil, nil
	}

	out := make([]Task, 0, len(entries))
	for _, entry := range entries {
		if !matchesFilter(entry, filter) {
			continue
		}
		taskPath := filepath.Join(root, "tasks", "ready", filepath.FromSlash(entry.Path))
		textBytes, err := os.ReadFile(taskPath)
		if err != nil {
			return nil, fmt.Errorf("read task %s: %w", entry.Path, err)
		}
		text := string(textBytes)
		task := Task{
			Path:          filepath.ToSlash(filepath.Join("tasks", "ready", entry.Path)),
			BacklogStatus: entry.Status,
			Tags:          slices.Clone(entry.Tags),
			Verdict:       parseVerdict(text),
			DoneScope:     parseScopeLine(text, "done scope"),
			NotDoneScope:  parseScopeLine(text, "not-done scope"),
			Claims:        parseClaims(text),
		}
		out = append(out, task)
	}
	return out, nil
}

func Check(root string) ([]Diagnostic, error) {
	tasks, err := LoadBacklogTasks(root, TaskFilter{RequireAnyTag: []string{"P0"}})
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	for _, task := range tasks {
		if task.Verdict == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: task.Path,
				Message:  "P0 task must define Scope Verdict: - verdict: `not_started|in_progress|implemented|proven|done_scoped`",
			})
		}
		if strings.TrimSpace(task.DoneScope) == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: task.Path,
				Message:  "P0 task must define Scope Verdict: - done scope:",
			})
		}
		if strings.TrimSpace(task.NotDoneScope) == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: task.Path,
				Message:  "P0 task must define Scope Verdict: - not-done scope:",
			})
		}
		if len(task.Claims) == 0 {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: task.Path,
				Message:  "P0 task must include Claim Ledger entries: - [ ] claim: ... | proof: ... | status: ... | validated: YYYY-MM-DD",
			})
			continue
		}
		if task.BacklogStatus == BacklogStatusDone && task.Verdict != VerdictDoneScoped {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: task.Path,
				Message:  "backlog marks P0 task DONE but Scope Verdict is not `done_scoped`",
			})
		}
		if task.Verdict == VerdictDoneScoped {
			for _, claim := range task.Claims {
				if !claim.Checked || claim.Status != ClaimStatusProven {
					diagnostics = append(diagnostics, Diagnostic{
						Filename: task.Path,
						Message:  "Scope Verdict `done_scoped` requires every claim checked and status `proven`",
					})
					break
				}
			}
		}
	}
	slices.SortFunc(diagnostics, func(a, b Diagnostic) int {
		if a.Filename != b.Filename {
			return strings.Compare(a.Filename, b.Filename)
		}
		return strings.Compare(a.Message, b.Message)
	})
	return diagnostics, nil
}

func BuildReport(tasks []Task) string {
	if len(tasks) == 0 {
		return "p0 progress report: no P0 tasks"
	}
	var b strings.Builder
	b.WriteString("p0 progress report\n")
	b.WriteString("path | backlog | verdict | proven/claims\n")
	for _, task := range tasks {
		proven := 0
		for _, claim := range task.Claims {
			if claim.Status == ClaimStatusProven {
				proven++
			}
		}
		fmt.Fprintf(&b, "%s | %s | %s | %d/%d\n", task.Path, task.BacklogStatus, task.Verdict, proven, len(task.Claims))
	}
	return strings.TrimRight(b.String(), "\n")
}

func parseScopeLine(taskText, key string) string {
	section, ok := markdownSection(taskText, "Scope Verdict")
	if !ok {
		return ""
	}
	prefix := "- " + key + ":"
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	}
	return ""
}

func parseVerdict(taskText string) Verdict {
	raw := strings.Trim(parseScopeLine(taskText, "verdict"), "` ")
	switch Verdict(raw) {
	case VerdictNotStarted, VerdictInProgress, VerdictImplemented, VerdictProven, VerdictDoneScoped:
		return Verdict(raw)
	default:
		return ""
	}
}

func parseClaims(taskText string) []Claim {
	section, ok := markdownSection(taskText, "Claim Ledger")
	if !ok {
		return nil
	}
	var claims []Claim
	for _, line := range strings.Split(section, "\n") {
		match := claimLineRE.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 6 {
			continue
		}
		claims = append(claims, Claim{
			Checked:   strings.EqualFold(match[1], "x"),
			Claim:     strings.TrimSpace(match[2]),
			Proof:     strings.TrimSpace(match[3]),
			Status:    ClaimStatus(strings.TrimSpace(match[4])),
			Validated: strings.TrimSpace(match[5]),
		})
	}
	return claims
}

func markdownSection(text, sectionName string) (string, bool) {
	lines := strings.Split(text, "\n")
	inSection := false
	var section []string
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

type p0Entry struct {
	Path   string
	Status BacklogStatus
	Tags   []string
}

func backlogEntries(backlog string) []p0Entry {
	seen := map[string]struct{}{}
	var out []p0Entry
	for _, line := range strings.Split(backlog, "\n") {
		match := backtickTaskRE.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		path := filepath.ToSlash(filepath.Clean(match[1]))
		if path == "." || strings.HasPrefix(path, "../") {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		tags := parseBacklogTags(line)
		status := BacklogStatusOther
		if containsTag(tags, "DONE") {
			status = BacklogStatusDone
		} else if containsTag(tags, "PENDING") {
			status = BacklogStatusPending
		}
		out = append(out, p0Entry{
			Path:   path,
			Status: status,
			Tags:   tags,
		})
	}
	slices.SortFunc(out, func(a, b p0Entry) int {
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

func parseBacklogTags(line string) []string {
	tagSet := map[string]struct{}{}
	for _, token := range strings.Split(line, "**") {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" || strings.Contains(trimmed, "`") || strings.Contains(trimmed, ".md") {
			continue
		}
		for _, part := range strings.Fields(trimmed) {
			if part == "" {
				continue
			}
			tagSet[part] = struct{}{}
		}
	}
	var out []string
	for tag := range tagSet {
		out = append(out, tag)
	}
	slices.Sort(out)
	return out
}

func matchesFilter(entry p0Entry, filter TaskFilter) bool {
	if strings.TrimSpace(filter.PathPrefix) != "" {
		prefix := filepath.ToSlash(filepath.Clean(filter.PathPrefix))
		if !strings.HasPrefix(entry.Path, prefix) {
			return false
		}
	}
	if len(filter.RequireAnyTag) == 0 {
		return true
	}
	for _, required := range filter.RequireAnyTag {
		for _, tag := range entry.Tags {
			if strings.EqualFold(strings.TrimSpace(tag), strings.TrimSpace(required)) {
				return true
			}
		}
	}
	return false
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
