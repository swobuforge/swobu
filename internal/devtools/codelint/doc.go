// Package codelint defines Swobu's production-code size cap checker.
//
// It enforces a small set of repo-global hard caps over production Go code:
// oversized files, oversized functions, and high-complexity functions. It
// also enforces TUI app-userspace boundaries (no direct .BuildView calls, BuildView
// returns view.ViewSpec as the ViewBuilder contract instead of
// view.RenderNode, and no direct layout imports from app
// packages) plus a TUI clipboard commodity rule
// (no command-probed clipboard implementations). TUI boundary and clipboard
// commodity rules are non-ignorable, except for the shell-local targeted
// boundary exception in app/views/shell.go. The checker also requests package
// type information so type-check diagnostics are surfaced in lint output. It
// intentionally skips tests, legacy code, and repo devtools so the rule stays
// focused on current product code rather than generating a large exception set.
//
// It also enforces a redundant-model-trim rule: strings.TrimSpace on model
// fields in view and selector code is forbidden. The law is: model fields
// are always trimmed at the write boundary (reducers/effects). Trimming on
// read is redundant and signals that the boundary invariant is not trusted.
//
// It also enforces a stale-reference rule over production code: references to
// legacy or retired terms in imports, comments, and string literals are
// forbidden so the active codebase remains evergreen.
//
// It also enforces a no-empty-interface rule in production code. Raw
// `interface{}` is forbidden; code must use concrete types or explicit `any`
// where dynamic boundary shapes are intentionally required.
package codelint
