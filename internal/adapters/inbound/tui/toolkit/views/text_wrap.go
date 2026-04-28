package views

import (
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/layout"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
)

// WrapLinePreserveIndent wraps long text while preserving indentation.
func WrapLinePreserveIndent(line string, width int) []string {
	return layout.WrapLinePreserveIndent(line, width)
}

// WrapLineRowsPreserveIndent wraps one line and maps each segment into a row.
func WrapLineRowsPreserveIndent[M any](line string, width int, buildRow func(string) view.ViewSpec[M]) []view.ViewSpec[M] {
	if buildRow == nil {
		return nil
	}
	segments := WrapLinePreserveIndent(line, width)
	rows := make([]view.ViewSpec[M], 0, len(segments))
	for _, segment := range segments {
		rows = append(rows, buildRow(segment))
	}
	return rows
}
