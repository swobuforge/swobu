package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// WrapLinePreserveIndent wraps long text while preserving indentation.
func WrapLinePreserveIndent(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	return layout.WrapLinePreserveIndent(line, width)
}

// WrapLineRowsPreserveIndent wraps one line and maps each segment into a row.
func WrapLineRowsPreserveIndent[M any](line string, width int, buildRow func(string) retained.ViewSpec[M]) []retained.ViewSpec[M] {
	if buildRow == nil {
		return nil
	}
	segments := WrapLinePreserveIndent(line, width)
	rows := make([]retained.ViewSpec[M], 0, len(segments))
	for _, segment := range segments {
		rows = append(rows, buildRow(segment))
	}
	return rows
}
