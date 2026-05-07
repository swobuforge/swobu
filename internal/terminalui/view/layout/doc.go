// Package layout defines strategy-neutral layout algebra and value objects
// shared by terminal view builders.
//
// The canonical spatial unit is Cell (terminal columns/rows). Layout
// composition should be expressed in these value objects instead of ad hoc
// rune/byte arithmetic in app/toolkit code.
//
// It intentionally contains no retained-engine render node types.
package layout
