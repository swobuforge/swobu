package codelint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFailsOnLargeFile(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", sampleSource("", 410, `package sample

func ok() {}
`))
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "file has") {
		t.Fatalf("messages = %q, want file-length warning", joined)
	}
}

func TestCheckFailsOnLongFunction(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `package sample

func oversized() int {
	total := 0
`+repeatLine("\ttotal++\n", 180)+`	return total
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, `function "oversized" has`) {
		t.Fatalf("messages = %q, want function-length warning", joined)
	}
}

func TestCheckFailsOnHighComplexity(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `package sample

func branching(v int) int {
	if v == 0 { v++ }
	if v == 1 { v++ }
	if v == 2 { v++ }
	if v == 3 { v++ }
	if v == 4 { v++ }
	if v == 5 { v++ }
	if v == 6 { v++ }
	if v == 7 { v++ }
	if v == 8 { v++ }
	if v == 9 { v++ }
	if v == 10 { v++ }
	if v == 11 { v++ }
	if v == 12 { v++ }
	if v == 13 { v++ }
	if v == 14 { v++ }
	if v == 15 { v++ }
	if v == 16 { v++ }
	if v == 17 { v++ }
	if v == 18 { v++ }
	if v == 19 { v++ }
	if v == 20 { v++ }
	if v == 21 { v++ }
	if v == 22 { v++ }
	if v == 23 { v++ }
	if v == 24 { v++ }
	return v
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "cyclomatic complexity") {
		t.Fatalf("messages = %q, want complexity warning", joined)
	}
}

func TestCheckHonorsIgnoreDirectives(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", sampleSource(
		"// swobu:codelint ignore file-length\n",
		410,
		`package sample

// swobu:codelint ignore function-length
// swobu:codelint ignore complexity
func justified() int {
	total := 0
`+repeatLine("\ttotal++\n", 180)+`
	if total == 0 { total++ }
	if total == 1 { total++ }
	if total == 2 { total++ }
	if total == 3 { total++ }
	if total == 4 { total++ }
	if total == 5 { total++ }
	if total == 6 { total++ }
	if total == 7 { total++ }
	if total == 8 { total++ }
	if total == 9 { total++ }
	if total == 10 { total++ }
	if total == 11 { total++ }
	if total == 12 { total++ }
	if total == 13 { total++ }
	if total == 14 { total++ }
	if total == 15 { total++ }
	if total == 16 { total++ }
	if total == 17 { total++ }
	if total == 18 { total++ }
	if total == 19 { total++ }
	if total == 20 { total++ }
	if total == 21 { total++ }
	if total == 22 { total++ }
	if total == 23 { total++ }
	if total == 24 { total++ }
	return total
}
`))
	joined := joinMessages(diagnostics)
	if strings.Contains(joined, `function "justified" has`) {
		t.Fatalf("messages = %q, want no function-length warning", joined)
	}
	if strings.Contains(joined, "cyclomatic complexity") {
		t.Fatalf("messages = %q, want no complexity warning", joined)
	}
}

func TestCheckHonorsIgnoreDirectiveRationaleSuffix(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", sampleSource(
		"// swobu:codelint ignore file-length protocol codec fanout is transport-shaped\n",
		410,
		`package sample

// swobu:codelint ignore function-length operator codec has one fanout seam
// swobu:codelint ignore complexity transport branching maps wire variants
func justified() int {
	total := 0
`+repeatLine("\ttotal++\n", 180)+`
	if total == 0 { total++ }
	if total == 1 { total++ }
	if total == 2 { total++ }
	if total == 3 { total++ }
	if total == 4 { total++ }
	if total == 5 { total++ }
	if total == 6 { total++ }
	if total == 7 { total++ }
	if total == 8 { total++ }
	if total == 9 { total++ }
	if total == 10 { total++ }
	if total == 11 { total++ }
	if total == 12 { total++ }
	if total == 13 { total++ }
	if total == 14 { total++ }
	if total == 15 { total++ }
	if total == 16 { total++ }
	if total == 17 { total++ }
	if total == 18 { total++ }
	if total == 19 { total++ }
	if total == 20 { total++ }
	if total == 21 { total++ }
	if total == 22 { total++ }
	if total == 23 { total++ }
	if total == 24 { total++ }
	return total
}
`))
	if joined := joinMessages(diagnostics); joined != "" {
		t.Fatalf("messages = %q, want no diagnostics", joined)
	}
}

func TestCheckAllowsTUIAppUserspaceBuildViewCalls(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

type child struct{}

func (child) BuildView() int { return 1 }

func compose() int {
	return child{}.BuildView()
}
`)
	if joined := joinMessages(diagnostics); joined != "" {
		t.Fatalf("messages = %q, want no diagnostics", joined)
	}
}

func TestCheckFailsOnTUIAppUserspaceLayoutImport(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelinttest\n\ngo 1.22\n")
	writeModuleFile(t, root, "internal/terminalui/engine/retained/rendergraph/layout/layout.go", `package layout`)
	writeModuleFile(t, root, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import _ "example.com/codelinttest/internal/terminalui/engine/retained/rendergraph/layout"
`)

	diagnostics, err := checkDir(root, []string{"./..."})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "must not import engine/rendergraph/layout") {
		t.Fatalf("messages = %q, want tui userspace layout-import warning", joined)
	}
}

func TestCheckFailsOnTUIAppUserspaceRawLayoutAPIs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelinttest\n\ngo 1.22\n")
	writeModuleFile(t, root, "internal/terminalui/engine/retained/rendergraph/layout/layout.go", `package layout
type FlowChild struct{}
func NewColumn(...FlowChild) any { return nil }
`)
	writeModuleFile(t, root, "internal/terminalui/apps/cockpit/app/page/sample.go", `package page

import "example.com/codelinttest/internal/terminalui/engine/retained/rendergraph/layout"

func compose() any {
	return layout.NewColumn(layout.FlowChild{})
}
`)

	diagnostics, err := checkDir(root, []string{"./..."})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "must not use raw layout API") {
		t.Fatalf("messages = %q, want raw-layout-api warning", joined)
	}
}

func TestCheckAllowsTUIAppShellFileTargetedBoundaryIgnores(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelinttest\n\ngo 1.22\n")
	writeModuleFile(t, root, "internal/terminalui/engine/retained/rendergraph/layout/layout.go", `package layout
type FlowChild struct{}
`)
	writeModuleFile(t, root, "internal/terminalui/apps/cockpit/app/views/shell.go", `// swobu:codelint ignore tui-userspace-layout-import
// swobu:codelint ignore tui-userspace-layout-api
package views

import "example.com/codelinttest/internal/terminalui/engine/retained/rendergraph/layout"

func sample() {
	_ = layout.FlowChild{}
}
`)

	diagnostics, err := checkDir(root, []string{"./..."})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if joined := joinMessages(diagnostics); strings.Contains(joined, "tui userspace boundary rules do not support") {
		t.Fatalf("messages = %q, want shell-local targeted ignore to be permitted", joined)
	}
	if joined := joinMessages(diagnostics); strings.Contains(joined, "must not import engine/rendergraph/layout") || strings.Contains(joined, "must not use raw layout API") {
		t.Fatalf("messages = %q, want shell-local targeted ignore to suppress those boundary diagnostics", joined)
	}
}

func TestCheckRejectsIgnoreDirectiveForTUIUserspaceBoundaryRules(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/app/views/sample.go", `// swobu:codelint ignore tui-userspace-layout-import
package views

import "example.com/codelinttest/internal/terminalui/engine/retained/rendergraph/layout"

func compose() any { return layout.FlowChild{} }
`)
	joined := joinMessages(diagnostics)
	if !strings.Contains(joined, "do not support swobu:codelint ignore") {
		t.Fatalf("messages = %q, want non-ignorable tui userspace boundary warning", joined)
	}
	if !strings.Contains(joined, "must not import engine/rendergraph/layout") {
		t.Fatalf("messages = %q, want layout-import warning even with ignore marker", joined)
	}
}

func TestCheckFailsOnTUIUserspaceLayoutSkinLiteralInSemanticRows(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import "example.com/codelinttest/internal/terminalui/toolkit/views"

func bad() any {
	return views.ListItemRow[struct{}]("   label", false, false, false, nil, nil)
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "must not pass layout skin literals") {
		t.Fatalf("messages = %q, want layout-skin literal warning", joined)
	}
}

func TestCheckAllowsTUIUserspaceInsetLabelWithoutSkinLiteral(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import "example.com/codelinttest/internal/terminalui/toolkit/views"

func ok(label string) string {
	return views.InsetLabel(label, 3)
}
`)
	if joined := joinMessages(diagnostics); strings.Contains(joined, "must not pass layout skin literals") {
		t.Fatalf("messages = %q, want no layout-skin literal warning", joined)
	}
}

func TestCheckFailsOnTerminalUIEngineImportingApps(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/engine/retained/loop/sample.go", `package loop

import _ "example.com/codelinttest/internal/terminalui/apps/cockpit/app/state/model"
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, `terminalui dependency law violation: lane "engine" must not import lane "apps"`) {
		t.Fatalf("messages = %q, want terminalui dependency law engine->apps warning", joined)
	}
}

func TestCheckFailsOnTerminalUIToolkitImportingApps(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/toolkit/views/sample.go", `package views

import _ "example.com/codelinttest/internal/terminalui/apps/cockpit/app/state/model"
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, `terminalui dependency law violation: lane "toolkit" must not import lane "apps"`) {
		t.Fatalf("messages = %q, want terminalui dependency law toolkit->apps warning", joined)
	}
}

func TestCheckAllowsTerminalUIAppsImportingToolkitAndEngine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelinttest\n\ngo 1.22\n")
	writeModuleFile(t, root, "internal/terminalui/toolkit/views/node.go", "package views\ntype Node struct{}\n")
	writeModuleFile(t, root, "internal/terminalui/engine/retained/view/node.go", "package view\ntype Node struct{}\n")
	writeModuleFile(t, root, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import (
	_ "example.com/codelinttest/internal/terminalui/toolkit/views"
	_ "example.com/codelinttest/internal/terminalui/engine/retained/view"
)
`)

	diagnostics, err := checkDir(root, []string{"./..."})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if joined := joinMessages(diagnostics); strings.Contains(joined, "terminalui dependency law violation") {
		t.Fatalf("messages = %q, want no terminalui dependency law warning for apps imports", joined)
	}
}

func TestCheckRejectsIgnoreDirectiveForTerminalUIDependencyLaw(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/engine/retained/loop/sample.go", `// swobu:codelint ignore tui-dependency-law
package loop

import _ "example.com/codelinttest/internal/terminalui/apps/cockpit/app/state/model"
`)
	joined := joinMessages(diagnostics)
	if !strings.Contains(joined, "terminalui dependency law does not support swobu:codelint ignore") {
		t.Fatalf("messages = %q, want non-ignorable terminalui dependency law warning", joined)
	}
	if !strings.Contains(joined, `terminalui dependency law violation: lane "engine" must not import lane "apps"`) {
		t.Fatalf("messages = %q, want dependency-law warning even with ignore marker", joined)
	}
}

func TestCheckRejectsIgnoreDirectiveForTUIUserspaceLayoutSkinRule(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/app/views/sample.go", `// swobu:codelint ignore tui-userspace-layout-skin
package views

import "example.com/codelinttest/internal/terminalui/toolkit/views"

func bad() any {
	return views.ListItemRow[struct{}]("   label", false, false, false, nil, nil)
}
`)
	joined := joinMessages(diagnostics)
	if !strings.Contains(joined, "do not support swobu:codelint ignore") {
		t.Fatalf("messages = %q, want non-ignorable tui userspace boundary warning", joined)
	}
	if !strings.Contains(joined, "must not pass layout skin literals") {
		t.Fatalf("messages = %q, want layout-skin literal warning even with ignore marker", joined)
	}
}

func TestCheckFailsOnLongSleep(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `package sample

import "time"

func slowSleep() {
	time.Sleep(2 * time.Second)
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "time.Sleep >= 1s is forbidden") {
		t.Fatalf("messages = %q, want long-sleep warning", joined)
	}
}

func TestCheckFailsOnEmptyInterfaceType(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `package sample

func bad(v interface{}) interface{} { return v }
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "empty interface type is forbidden") {
		t.Fatalf("messages = %q, want empty-interface warning", joined)
	}
}

func TestCheckAllowsAnyAliasForDynamicBoundaryShapes(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `package sample

func ok(v any) any { return v }
`)
	if joined := joinMessages(diagnostics); strings.Contains(joined, "empty interface type is forbidden") {
		t.Fatalf("messages = %q, want no empty-interface warning", joined)
	}
}

func TestCheckFailsOnProtocolTokenInCanonicalRequestTypeName(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/domain/compatibility/request.go", `package compatibility

type ResponsesRequest struct{}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, `embeds protocol token "Responses"`) {
		t.Fatalf("messages = %q, want canonical semantic type naming warning", joined)
	}
}

func TestCheckAllowsSemanticCanonicalRequestTypeName(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/domain/compatibility/request.go", `package compatibility

type DialogCanonicalRequest struct{}
`)
	if joined := joinMessages(diagnostics); strings.Contains(joined, "canonical semantic type name") {
		t.Fatalf("messages = %q, want no canonical semantic type naming warning", joined)
	}
}

func TestCheckRejectsIgnoreDirectiveForCanonicalSemanticTypeNamingRule(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/domain/compatibility/request.go", `// swobu:codelint ignore canonical-semantic-type-name
package compatibility

type DialogCanonicalRequest struct{}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "canonical semantic type naming rule does not support swobu:codelint ignore") {
		t.Fatalf("messages = %q, want non-ignorable canonical semantic naming warning", joined)
	}
}

func TestCheckAllowsShortSleep(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `package sample

import "time"

func shortSleep() {
	time.Sleep(50 * time.Millisecond)
}
`)
	if joined := joinMessages(diagnostics); strings.Contains(joined, "time.Sleep >= 1s is forbidden") {
		t.Fatalf("messages = %q, want no long-sleep warning", joined)
	}
}

func TestCheckHonorsLongSleepIgnoreDirective(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `// swobu:codelint ignore long-sleep
package sample

import "time"

func ignoredSleep() {
	time.Sleep(3 * time.Second)
}
`)
	if joined := joinMessages(diagnostics); strings.Contains(joined, "time.Sleep >= 1s is forbidden") {
		t.Fatalf("messages = %q, want long-sleep diagnostic ignored", joined)
	}
}

func TestCheckFailsOnTUIUserspaceDirectGeometryWrapperCalls(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelinttest\n\ngo 1.22\n")
	writeModuleFile(t, root, "internal/terminalui/engine/retained/view/view.go", `package view
type RenderNode interface{}
type Context[M any] struct{}
type ViewSpec[M any] interface{}
func Column[M any](ctx *Context[M], kids ...ViewSpec[M]) ViewSpec[M] { return nil }
func Padded[M any](child ViewSpec[M], top, right, bottom, left int) ViewSpec[M] { return child }
`)
	writeModuleFile(t, root, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import "example.com/codelinttest/internal/terminalui/engine/retained/view"

type noop struct{}
func (noop) BuildView(*view.Context[struct{}]) view.RenderNode { return nil }

func bad() view.ViewSpec[struct{}] {
	return view.Column[struct{}](nil, view.Padded(noop{}, 0, 0, 0, 2))
}
`)
	diagnostics, err := checkDir(root, []string{"./..."})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "must compose geometry with view-transform helpers (view.With*)") {
		t.Fatalf("messages = %q, want geometry wrapper warning", joined)
	}
}

func TestCheckAllowsTUIUserspaceGeometryThroughWithTransforms(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelinttest\n\ngo 1.22\n")
	writeModuleFile(t, root, "internal/terminalui/engine/retained/view/view.go", `package view
type RenderNode interface{}
type Context[M any] struct{}
type ViewSpec[M any] interface{}
func WithPadLeft[M any](left int) func(ViewSpec[M]) ViewSpec[M] { return func(base ViewSpec[M]) ViewSpec[M] { return base } }
`)
	writeModuleFile(t, root, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import "example.com/codelinttest/internal/terminalui/engine/retained/view"

type noop struct{}
func (noop) BuildView(*view.Context[struct{}]) view.RenderNode { return nil }

func ok() view.ViewSpec[struct{}] {
	return view.WithPadLeft[struct{}](2)(noop{})
}
`)
	diagnostics, err := checkDir(root, []string{"./..."})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if joined := joinMessages(diagnostics); strings.Contains(joined, "must compose geometry with view-transform helpers (view.With*)") {
		t.Fatalf("messages = %q, want no geometry wrapper warning", joined)
	}
}

func TestCheckSurfacesTypeErrorsFromLoadedPackages(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `package sample

func broken() {
	var n int = "nope"
	_ = n
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "cannot use") {
		t.Fatalf("messages = %q, want type-check diagnostic", joined)
	}
}

func TestCheckFailsOnTUIAppUserspaceBuildReturningNode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelinttest\n\ngo 1.22\n")
	writeModuleFile(t, root, "internal/terminalui/engine/retained/view/view.go", `package view
type Context[T any] struct{}
type RenderNode interface{}
type ViewSpec[T any] interface{}
func Render[T any](_ *Context[T], _ ViewSpec[T]) RenderNode { return nil }
`)
	writeModuleFile(t, root, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import "example.com/codelinttest/internal/terminalui/engine/retained/view"

type W struct{}

func (W) BuildView(ctx *view.Context[struct{}], a view.ViewSpec[struct{}], b view.ViewSpec[struct{}]) view.RenderNode {
	if a != nil {
		return view.Render(ctx, a)
	}
	return view.Render(ctx, b)
}
`)

	diagnostics, err := checkDir(root, []string{"./..."})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "BuildView must return view.ViewSpec, not view.RenderNode") {
		t.Fatalf("messages = %q, want Build return signature warning", joined)
	}
}

func TestCheckFailsOnTUIClipboardCommandProbeImplementation(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/clipboard.go", `package tui

import "os/exec"

func copy(text string) error {
	_, err := exec.LookPath("pbcopy")
	return err
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "must use golang.design/x/clipboard") {
		t.Fatalf("messages = %q, want clipboard commodity warning", joined)
	}
}

func TestCheckRejectsIgnoreDirectiveForTUIClipboardCommodityRule(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/clipboard.go", `// swobu:codelint ignore tui-clipboard-command-probe
package tui

func copy(text string) string {
	return text
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "clipboard commodity rule does not support swobu:codelint ignore") {
		t.Fatalf("messages = %q, want non-ignorable clipboard commodity warning", joined)
	}
}

func TestCheckFailsOnLegacyOrDeprecatedReferences(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `package sample

import _ "example.com/codelinttest/legacy/adapter"

// deprecated path kept for now
const note = "legacy behavior is forbidden"
`)
	joined := joinMessages(diagnostics)
	if !strings.Contains(joined, `forbidden stale term "legacy"`) {
		t.Fatalf("messages = %q, want legacy stale-reference diagnostic", joined)
	}
	if !strings.Contains(joined, `forbidden stale term "deprecated"`) {
		t.Fatalf("messages = %q, want deprecated stale-reference diagnostic", joined)
	}
}

func TestCheckRejectsIgnoreDirectiveForStaleReferencesRule(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "sample/sample.go", `// swobu:codelint ignore stale-references
package sample

const note = "deprecated API"
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "stale-reference rule does not support swobu:codelint ignore") {
		t.Fatalf("messages = %q, want non-ignorable stale-reference warning", joined)
	}
}

func TestCheckFailsOnRedundantModelTrimInView(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import "strings"

type Model struct {
	Name string
}

func build(model Model) string {
	return strings.TrimSpace(model.Name)
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "redundant strings.TrimSpace on model field") {
		t.Fatalf("messages = %q, want redundant model trim warning", joined)
	}
}

func TestCheckFailsOnRedundantModelTrimInSelector(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/app/selectors/sample.go", `package selectors

import "strings"

type Model struct {
	Hint string
}

func HeaderHint(model Model) string {
	return strings.TrimSpace(model.Hint)
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "redundant strings.TrimSpace on model field") {
		t.Fatalf("messages = %q, want redundant model trim warning", joined)
	}
}

func TestCheckFailsOnNestedModelFieldTrim(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import "strings"

type Config struct {
	Spec string
}

type Model struct {
	Config Config
}

func build(model Model) string {
	return strings.TrimSpace(model.Config.Spec)
}
`)
	if joined := joinMessages(diagnostics); !strings.Contains(joined, "redundant strings.TrimSpace on model field") {
		t.Fatalf("messages = %q, want redundant model trim warning for nested field", joined)
	}
}

func TestCheckAllowsTrimSpaceOnNonModelFields(t *testing.T) {
	t.Parallel()

	diagnostics := runCheckInTempModule(t, "internal/terminalui/apps/cockpit/app/views/sample.go", `package views

import "strings"

type Model struct {
	Name string
}

func build(value string) string {
	return strings.TrimSpace(value)
}
`)
	if joined := joinMessages(diagnostics); joined != "" {
		t.Fatalf("messages = %q, want no diagnostic for non-model field trim", joined)
	}
}

func sampleSource(prefix string, padLines int, body string) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(body)
	for i := 0; i < padLines; i++ {
		b.WriteString("// filler\n")
	}
	return b.String()
}

func repeatLine(line string, count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		b.WriteString(line)
	}
	return b.String()
}

func writeModuleFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", relPath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", relPath, err)
	}
}

func joinMessages(diagnostics []Diagnostic) string {
	parts := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		parts = append(parts, diagnostic.Message)
	}
	return strings.Join(parts, "\n")
}

func runCheckInTempModule(t *testing.T, relPath, content string) []Diagnostic {
	t.Helper()

	root := t.TempDir()
	writeModuleFile(t, root, "go.mod", "module example.com/codelinttest\n\ngo 1.22\n")
	writeModuleFile(t, root, relPath, content)

	diagnostics, err := checkDir(root, []string{"./..."})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	return diagnostics
}
