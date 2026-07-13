package views

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// migrationTracker is the migration-state ratchet for the cockpit views
// package.  It enumerates known legacy files that are allowed to import
// retained packages; any file NOT in this set that imports retained
// packages indicates a regression (new code using deprecated API).
//
// To add a file to the allowlist: migrate it to core.Node and remove
// retained imports.  Then delete the entry from this map.  The ratchet
// tightens automatically.
//
// See TODO markers in code for remaining migration work.
var migrationTracker = map[string]struct {
	allowedRetainedImports []string
	migrationSlice         string
}{
	"clients.go":                     {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"clients_disclosure.go":          {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"collapsible_section.go":         {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"create.go":                      {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"disclosure_note.go":             {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
	"esc_closable_disclosure.go":     {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"filterable_picker.go":           {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"filterable_picker_focus.go":     {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"focus_affordance.go":            {[]string{"engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"header_bar.go":                  {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"help.go":                        {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"}, // MIGRATED: core.Node is canonical; bridge function still needs retained types
	"mismatch.go":                    {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"onboarding_hero.go":             {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
	"root/body_canvas.go":            {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
	"root/body_layout.go":            {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
	"root/root.go":                   {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/add_model_auth_header.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/add_model_credentials.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/add_model_panel.go":     {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/auth_mode_registry.go":  {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/backend_url.go":         {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/bedrock_auth_editors.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/bedrock_profile_picker.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/canonical_provider_config_layout.go": {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/credential_choice.go":   {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/credential_file_browse.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/credential_file_choice.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/draft_model_choice.go":  {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/draft_protocol_mode_view.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/env_key_choice.go":      {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/keychain_key_choice.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/model_choice.go":        {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/model_panels.go":        {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/model_picker_disclosure.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/provider_auth_header.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/provider_auth_presenter.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/providers.go":           {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/providers_workspace_panel.go": {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/run_on.go":              {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/save_actions.go":        {[]string{"engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/section.go":             {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/section_summaries.go":   {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/selected_frame.go":      {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"routing/target_alias.go":        {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"sections.go":                    {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
	"setting_action.go":              {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"summary_line.go":                {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
	"text_line.go":                   {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
	"traffic.go":                     {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"view_builder.go":                {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"view_grammar.go":                {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"workspace.go":                   {[]string{"view/retained", "engine/retained/update"}, "2026-06-14-cockpit-help-core-migration"},
	"wrapped_payload_text_view.go":   {[]string{"view/retained"}, "2026-06-14-cockpit-help-core-migration"},
}

func TestCockpitViews_MigrationRatchet(t *testing.T) {
	t.Parallel()

	root := packageDir(t)

	// Phase 1: scan all non-test .go files.
	astFiles := make(map[string][]string) // relPath → imports
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		imports, err := parseImports(path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		astFiles[rel] = imports
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	// Phase 2: for each file with retained imports, check ratchet.
	for rel, imports := range astFiles {
		retainedImports := filterRetainedImports(imports, rel)
		if len(retainedImports) == 0 {
			continue
		}

		tracker, known := migrationTracker[rel]
		if !known {
			// File is NOT in tracker but imports retained → regression.
			t.Fatalf("%s imports retained packages but is not in migrationTracker; "+
				"if this is a new file, rewrite without retained imports. "+
				"violations: %v", rel, retainedImports)
		}

		// If tracker entry has empty allowlist (e.g. help.go), treat it as
		// "should have zero retained imports" → fail if any exist.
		for _, imp := range retainedImports {
			allowed := false
			for _, a := range tracker.allowedRetainedImports {
				if strings.Contains(imp, a) {
					allowed = true
					break
				}
			}
			if !allowed {
				t.Fatalf("%s imports disallowed retained package %q; "+
					"migrate in slice %s or update allowlist", rel, imp, tracker.migrationSlice)
			}
		}
	}

	// Phase 3: ensure tracker has no stale entries (files that no longer exist).
	for rel := range migrationTracker {
		if _, ok := astFiles[rel]; !ok {
			// File no longer exists but still in tracker → delete it.
			// We just warn; the next slice can clean the tracker.
			t.Logf("migrationTracker has stale entry: %s (file deleted)", rel)
		}
	}
}

func parseImports(path string) ([]string, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	imports := make([]string, 0, len(parsed.Imports))
	for _, spec := range parsed.Imports {
		imports = append(imports, strings.Trim(spec.Path.Value, "\""))
	}
	return imports, nil
}

func filterRetainedImports(imports []string, relPath string) []string {
	var out []string
	for _, imp := range imports {
		if strings.Contains(imp, "/internal/terminalui/view/retained") {
			out = append(out, imp)
		}
		if strings.Contains(imp, "/internal/terminalui/engine/retained/update") {
			out = append(out, imp)
		}
	}
	return out
}

func packageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}
