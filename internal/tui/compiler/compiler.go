package compiler

import (
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/swobuforge/swobu/internal/tui/core"
)

// ── Environment ───────────────────────────────────────────────────────

// WidthBand classifies terminal width for layout decisions.
type WidthBand int

const (
	WidthNarrow WidthBand = iota  // ≤ 60
	WidthStandard                 // 61–100
	WidthWide                     // 100+
)

// ColorMode classifies terminal color support.
type ColorMode int

const (
	ColorNone ColorMode = iota // no colors
	Color16                    // 16 ANSI colors
	Color256                   // 256 colors
	ColorTrue                  // 24-bit truecolor
)

// GlyphTier classifies Unicode support.
type GlyphTier int

const (
	GlyphASCII GlyphTier = iota // ASCII only
	GlyphSafeUTF                // Latin-1 + common symbols
	GlyphStructuralUTF          // box drawing, arrows, block elements
)

// Environment carries render capabilities into the compiler. Tests must
// declare capability inputs explicitly.
type Environment struct {
	Width   WidthBand
	Color   ColorMode
	Glyphs  GlyphTier
	MaxSize core.Size
}

// DefaultEnvironment returns standard-width, truecolor, full-UTF capabilities.
func DefaultEnvironment() Environment {
	return Environment{Width: WidthStandard, Color: ColorTrue, Glyphs: GlyphStructuralUTF, MaxSize: core.Size{W: 80, H: 24}}
}

// ── Compiler ─────────────────────────────────────────────────────────

// Compiler[E] transforms a semantic Node[E] tree into compiled output.
// The zero value is usable.
type Compiler[E any] struct{}

// New creates a compiler.
func New[E any]() *Compiler[E] { return &Compiler[E]{} }

// CompileInput is everything the compiler needs.
type CompileInput[E any] struct {
	Root core.Node[E]
	Env  Environment
	// Focus carries runtime focus memory for scope-aware resolution.
	Focus core.FocusMemory
}

// CompiledFrame is all compiler output. Each field is inspectable and
// serializable without runtime or terminal dependencies.
type CompiledFrame[E any] struct {
	Frame             core.Frame
	FocusGraph        FocusGraph
	InteractionRoutes InteractionRouteTable[E]
	LayoutTree        LayoutTree
	StyleTable        StyleTable
	Diagnostics       core.Diagnostics
	Snapshot          Snapshot
}

// Compile runs the full pipeline: normalize → validate → style → layout →
// focus graph → interaction routes → paint → snapshot.
// If validation produces errors, layout and paint are skipped; diagnostics
// and the normalized tree are still returned in Snapshot.
func (c *Compiler[E]) Compile(in CompileInput[E]) CompiledFrame[E] {
	var out CompiledFrame[E]
	out.StyleTable = NewStyleTable()

	// 1. Normalize — canonical keys, auto FocusIDs, parent links.
	norm := normalizeNode(in.Root, core.Key(""), 0, nil)

	// 2. Validate — core semantic checks plus compiler-level rules.
	diags := core.Validate(in.Root)
	validateCompilerRules(norm, &diags)
	out.Diagnostics = diags

	// Build normalized tree for snapshot.
	out.Snapshot = buildSnapshot(norm, out)

	if diags.HasErrors() {
		return out
	}

	// 3. Resolve style — role + visual state → StyleID.
	resolveStyle(norm, &out.StyleTable)

	// 4. Resolve layout — Fit / Fixed / Fill rectangles.
	out.LayoutTree = resolveLayout(norm, in.Env.MaxSize)

	// 5. Build focus graph — visible targets, scope membership, enabled status.
	out.FocusGraph = buildFocusGraph(norm)

	// 6. Build interaction routes — scoped + global.
	out.InteractionRoutes = buildInteractionRoutes(norm)

	// 7. Paint — rectangles + styles → cell frame.
	out.Frame = paintFrame(out.LayoutTree, out.StyleTable, in.Env.MaxSize)

	// 8. Final snapshot after all passes.
	out.Snapshot = buildSnapshot(norm, out)

	return out
}

// ── Normalize ────────────────────────────────────────────────────────

// normalized wraps a node with compiler-augmented metadata.
type normalized[E any] struct {
	node   core.Node[E]
	key    core.Key
	depth  int
	parent *normalized[E]
}

func normalizeNode[E any](n core.Node[E], parentKey core.Key, depth int, parent *normalized[E]) normalized[E] {
	k := n.Key()
	if k.Empty() {
		k = parentKey.Child(fmt.Sprintf("auto-%d", depth))
	}
	if n.FocusID() == "" {
		n = n.WithFocusID(core.FocusID(k))
	}
	return normalized[E]{node: n, key: k, depth: depth, parent: parent}
}

// ── Compiler-level validation ────────────────────────────────────────

func validateCompilerRules[E any](n normalized[E], d *core.Diagnostics) {
	// Empty tree check is covered by core.Validate.
	// Layout-time checks: no nodes without size constraints that spill.
	// These are warnings because the compiler can choose defaults.
	for _, ch := range n.node.Children() {
		validateCompilerRules(normalizeNode(ch, n.key, n.depth+1, &n), d)
	}
}

// ── Style resolution ─────────────────────────────────────────────────

// StyleTable maps StyleID to resolved style metadata. No raw ANSI.
type StyleTable struct {
	// Styles are indexed by their stable hash. The map is not
	// exposed externally; paint queries by ID.
	nextID uint32
	defs   map[uint32]CellStyle
}

// CellStyle is the resolved style for a single cell. Terminal adapter
// maps this to ANSI or equivalent.
type CellStyle struct {
	ID          core.StyleID
	Role        core.Role
	States      core.VisualState
	ColorDepth  ColorMode
	GlyphTier   GlyphTier
}

// NewStyleTable creates an empty style table.
func NewStyleTable() StyleTable {
	return StyleTable{nextID: 1, defs: map[uint32]CellStyle{}}
}

// Resolve returns the cell style for a given ID, or the zero style if absent.
func (st StyleTable) Resolve(id core.StyleID) CellStyle {
	return st.defs[uint32(id)]
}

// Entries returns a deterministic slice of all defined styles.
func (st StyleTable) Entries() []CellStyle {
	var out []CellStyle
	for _, v := range st.defs {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (st *StyleTable) allocate(role core.Role, states core.VisualState, env Environment) core.StyleID {
	key := styleKey(role, states, env.Color, env.Glyphs)
	if existing, ok := st.defs[key]; ok {
		return existing.ID
	}
	id := core.StyleID(st.nextID)
	st.nextID++
	st.defs[key] = CellStyle{ID: id, Role: role, States: states, ColorDepth: env.Color, GlyphTier: env.Glyphs}
	return id
}

func styleKey(role core.Role, states core.VisualState, color ColorMode, glyphs GlyphTier) uint32 {
	// Stable hash combining role, states, color, and glyph tier.
	// Collisions are extremely unlikely with uint32 and v1 scope.
	h := uint32(2166136261)
	for _, b := range []byte(role) {
		h = (h ^ uint32(b)) * 16777619
	}
	h = (h ^ uint32(states)) * 16777619
	h = (h ^ uint32(color)) * 16777619
	h = (h ^ uint32(glyphs)) * 16777619
	return h
}

func resolveStyle[E any](n normalized[E], st *StyleTable) {
	if n.node.Kind() == "empty" {
		return
	}
	// Allocate a style for this node's resolved appearance.
	// Actual assignment to layout nodes happens during paint.
	st.allocate(n.node.Role(), n.node.VisualState(), Environment{})
	for _, ch := range n.node.Children() {
		resolveStyle(normalizeNode(ch, n.key, n.depth+1, &n), st)
	}
}

// ── Layout ───────────────────────────────────────────────────────────

// LayoutNode is the result of layout for one node.
type LayoutNode struct {
	Key    core.Key
	Kind   string
	Depth  int
	// Rect is the node's position and size in the terminal cell grid.
	Rect core.Rect
	// Offset is the visible line index (derived from depth-first order).
	Offset int
	Label  string
	Style  core.StyleID
	// Children indices into the LayoutTree.Nodes slice.
	ChildIndices []int
}

// Rect is an axis-aligned rectangle in cell coordinates.
type Rect struct{ X, Y, W, H int }

func (r Rect) empty() bool { return r.W == 0 || r.H == 0 }

// LayoutTree is the flattened layout output preserving parentage.
type LayoutTree struct {
	Nodes []LayoutNode
}

// Find returns the layout node for a given key, or a zero LayoutNode.
func (lt LayoutTree) Find(key core.Key) LayoutNode {
	for _, n := range lt.Nodes {
		if n.Key == key {
			return n
		}
	}
	return LayoutNode{}
}

// resolveLayout produces a LayoutTree by walking the normalized tree and
// assigning rectangles. v1 layout uses a simple constraint model:
//
//   Fit   → size from content (text label width, or wrap to container)
//   Fixed → exact cell count
//   Fill  → proportional share of remaining space
func resolveLayout[E any](n normalized[E], bounds core.Size) LayoutTree {
	var lt LayoutTree
	var offset int
	var walk func(normalized[E], core.Rect) int
	walk = func(cur normalized[E], parentRect core.Rect) int {
		if cur.node.Kind() == "empty" {
			return offset
		}
		nodeIdx := len(lt.Nodes)
		lt.Nodes = append(lt.Nodes, LayoutNode{
			Key:    cur.key,
			Kind:   cur.node.Kind(),
			Depth:  cur.depth,
			Offset: offset,
			Label:  cur.node.Label(),
		})
		offset++

		// Compute this node's rectangle within the parent.
		rect := computeNodeRect(cur, parentRect)
		lt.Nodes[nodeIdx].Rect = rect

		// Recurse into children.
		if !cur.node.IsCollapsed() {
			childRect := rect // children occupy parent's interior (v1: no padding)
			for _, ch := range cur.node.Children() {
				childIdx := walk(normalizeNode(ch, cur.key, cur.depth+1, &cur), childRect)
				if childIdx >= 0 && childIdx < len(lt.Nodes) {
					lt.Nodes[nodeIdx].ChildIndices = append(lt.Nodes[nodeIdx].ChildIndices, childIdx)
				}
			}
		}
		return nodeIdx
	}
	walk(n, core.Rect{X: 0, Y: 0, W: bounds.W, H: bounds.H})
	return lt
}

// computeNodeRect resolves a node's rectangle within its parent's rectangle.
func computeNodeRect[E any](n normalized[E], parent core.Rect) core.Rect {
	w := resolveLength(n.node.Width(), parent.W)
	h := resolveLength(n.node.Height(), parent.H)

	// Clamp to parent bounds.
	if w > parent.W {
		w = parent.W
	}
	if h > parent.H {
		h = parent.H
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}

	return core.Rect{X: parent.X, Y: parent.Y, W: w, H: h}
}

// resolveLength interprets a Length constraint against an available dimension.
func resolveLength(l core.Length, available int) int {
	switch l.Kind {
	case core.SizeFit:
		return available // v1: Fit fills available space in container context
	case core.SizeFixed:
		if l.Value < 0 {
			return 0
		}
		return l.Value
	case core.SizeFill:
		// v1: Fill is treated as Fit (weight-based distribution deferred).
		return available
	default:
		return available
	}
}

// ── Focus Graph ──────────────────────────────────────────────────────

// FocusTarget represents one focusable or scope node in the visible tree.
type FocusTarget struct {
	ID        core.FocusID
	ScopeID   core.FocusScopeID
	Key       core.Key
	Kind      string
	Enabled   bool
	Depth     int
	ParentID  core.FocusID // empty if root focus target
	Children  []core.FocusID
}

// FocusGraph is the ordered set of visible focus targets.
type FocusGraph struct {
	Targets []FocusTarget
	// Index maps FocusID → index in Targets for O(1) lookup.
	Index map[core.FocusID]int
}

// Empty reports whether the graph has no targets.
func (g FocusGraph) Empty() bool { return len(g.Targets) == 0 }

// NextEnabled returns the next enabled focus target after fromID in visible order.
func (g FocusGraph) NextEnabled(fromID core.FocusID) (FocusTarget, bool) {
	idx, ok := g.Index[fromID]
	if !ok {
		return FocusTarget{}, false
	}
	for j := idx + 1; j < len(g.Targets); j++ {
		if g.Targets[j].Enabled {
			return g.Targets[j], true
		}
	}
	return FocusTarget{}, false
}

// PrevEnabled returns the previous enabled focus target before fromID.
func (g FocusGraph) PrevEnabled(fromID core.FocusID) (FocusTarget, bool) {
	idx, ok := g.Index[fromID]
	if !ok {
		return FocusTarget{}, false
	}
	for j := idx - 1; j >= 0; j-- {
		if g.Targets[j].Enabled {
			return g.Targets[j], true
		}
	}
	return FocusTarget{}, false
}

// FirstEnabled returns the first enabled target.
func (g FocusGraph) FirstEnabled() (FocusTarget, bool) {
	for _, t := range g.Targets {
		if t.Enabled {
			return t, true
		}
	}
	return FocusTarget{}, false
}

// LastEnabled returns the last enabled target.
func (g FocusGraph) LastEnabled() (FocusTarget, bool) {
	for i := len(g.Targets) - 1; i >= 0; i-- {
		if g.Targets[i].Enabled {
			return g.Targets[i], true
		}
	}
	return FocusTarget{}, false
}

// Find returns a target by ID.
func (g FocusGraph) Find(id core.FocusID) (FocusTarget, bool) {
	if idx, ok := g.Index[id]; ok {
		return g.Targets[idx], true
	}
	return FocusTarget{}, false
}

func buildFocusGraph[E any](n normalized[E]) FocusGraph {
	var g FocusGraph
	g.Index = map[core.FocusID]int{}
	var walk func(normalized[E], core.FocusID)
	walk = func(cur normalized[E], parentFocusID core.FocusID) {
		if cur.node.Kind() == "empty" {
			return
		}
		// Collapsed regions: include the region itself if focusable, but skip children.
		isFocusable := len(cur.node.Accepts()) > 0
		isScope := cur.node.ScopeID() != ""

		var myID core.FocusID
		if isFocusable || isScope {
			myID = cur.node.FocusID()
			if myID == "" {
				myID = core.FocusID(cur.key)
			}
			target := FocusTarget{
				ID:       myID,
				ScopeID:  cur.node.ScopeID(),
				Key:      cur.node.Key(),
				Kind:     cur.node.Kind(),
				Enabled:  !cur.node.IsDisabled() && isFocusable,
				Depth:    cur.depth,
				ParentID: parentFocusID,
			}
			g.Index[myID] = len(g.Targets)
			g.Targets = append(g.Targets, target)
			if parentFocusID != "" {
				if pIdx, ok := g.Index[parentFocusID]; ok {
					g.Targets[pIdx].Children = append(g.Targets[pIdx].Children, myID)
				}
			}
		}
		newParent := parentFocusID
		if isScope {
			newParent = myID
		}
		if cur.node.IsCollapsed() {
			return
		}
		for _, ch := range cur.node.Children() {
			walk(normalizeNode(ch, cur.key, cur.depth+1, &cur), newParent)
		}
	}
	walk(n, "")
	return g
}

// ── Interaction Routes ───────────────────────────────────────────────

// InteractionRoute maps an Intent to a FocusTarget ID and its typed signals.
type InteractionRoute[E any] struct {
	Intent      core.Intent
	FocusID     core.FocusID
	Category    RouteCategory
	Signals     []core.Signal[E]
	FieldChange func(string) E
	FieldCommit func(string) E
	FieldCancel func() E
}

// RouteCategory distinguishes how a route is resolved.
type RouteCategory int

const (
	RouteScoped RouteCategory = iota // resolved against current focus
	RouteGlobal                        // shortcut, fires regardless of focus
)

// GlobalShortcut is a shortcut that fires regardless of focus state or disabled status.
type GlobalShortcut[E any] struct {
	Intent  core.Intent
	Signal  core.Signal[E]
	Key     core.Key
	FocusID core.FocusID
}

// InteractionRouteTable is the resolved keymap + focus graph product.
type InteractionRouteTable[E any] struct {
	Routes    []InteractionRoute[E]
	Shortcuts []GlobalShortcut[E]
}

// Find returns the first scoped route matching intent and current focus.
func (t InteractionRouteTable[E]) Find(intent core.Intent, focusID core.FocusID) (InteractionRoute[E], bool) {
	for _, r := range t.Routes {
		if r.Intent == intent && r.FocusID == focusID {
			return r, true
		}
	}
	return InteractionRoute[E]{}, false
}

// FindGlobalShortcut returns the first global shortcut matching intent.
func (t InteractionRouteTable[E]) FindGlobalShortcut(intent core.Intent) (GlobalShortcut[E], bool) {
	for _, s := range t.Shortcuts {
		if s.Intent == intent {
			return s, true
		}
	}
	return GlobalShortcut[E]{}, false
}

func buildInteractionRoutes[E any](n normalized[E]) InteractionRouteTable[E] {
	var table InteractionRouteTable[E]
	var walk func(normalized[E])
	walk = func(cur normalized[E]) {
		if cur.node.Kind() == "empty" {
			return
		}
		if cur.node.IsCollapsed() {
			buildNodeRoutes(cur, &table)
			return
		}
		buildNodeRoutes(cur, &table)
		for _, ch := range cur.node.Children() {
			walk(normalizeNode(ch, cur.key, cur.depth+1, &cur))
		}
	}
	walk(n)
	return table
}

func buildNodeRoutes[E any](n normalized[E], table *InteractionRouteTable[E]) {
	// Scoped routes: only enabled nodes participate in focus-scoped routing.
	if !n.node.IsDisabled() && len(n.node.Accepts()) > 0 {
		fid := n.node.FocusID()
		if fid == "" {
			fid = core.FocusID(n.key)
		}
		for _, intent := range n.node.Accepts() {
			route := InteractionRoute[E]{
				Intent:   intent,
				FocusID:  fid,
				Category: RouteScoped,
				Signals:  n.node.Signals(),
			}
			if n.node.Kind() == "field" {
				route.FieldChange = n.node.OnChange()
				route.FieldCommit = n.node.OnCommit()
				route.FieldCancel = n.node.OnCancel()
			}
			table.Routes = append(table.Routes, route)
		}
	}
	// Global shortcuts: active regardless of focus or disabled status.
	for _, sc := range n.node.Shortcuts() {
		table.Shortcuts = append(table.Shortcuts, GlobalShortcut[E]{
			Intent:  sc.Intent,
			Signal:  sc.Signal,
			Key:     n.node.Key(),
			FocusID: n.node.FocusID(),
		})
	}
}

// ── Keymap ───────────────────────────────────────────────────────────

// Keymap translates raw input strings into semantic intents.
type Keymap struct {
	Bindings map[string]core.Intent
}

// DefaultKeymap returns the framework default keymap.
func DefaultKeymap() Keymap {
	return Keymap{Bindings: map[string]core.Intent{
		"enter":     core.IntentActivate,
		"esc":       core.IntentCancel,
		"tab":       core.IntentNextTab,
		"shift+tab": core.IntentPrevTab,
		"down":      core.IntentMoveNext,
		"up":        core.IntentMovePrevious,
		"left":      core.IntentMoveLeft,
		"right":     core.IntentMoveRight,
		"?":         core.IntentHelp,
		"ctrl+c":    core.IntentQuit,
	}}
}

// Resolve looks up a raw input string in the keymap.
func (km Keymap) Resolve(input string) (core.Intent, bool) {
	intent, ok := km.Bindings[input]
	return intent, ok
}

// ── Paint ────────────────────────────────────────────────────────────

// paintFrame produces a Frame from a resolved LayoutTree.
// Paint is the first pass allowed to think about terminal cells.
func paintFrame(lt LayoutTree, st StyleTable, bounds core.Size) core.Frame {
	cells := make([]core.Cell, bounds.W*bounds.H)
	for i := range lt.Nodes {
		n := &lt.Nodes[i]
		if n.Rect.Empty() {
			continue
		}
		// Clip to frame bounds.
		x0 := n.Rect.X
		y0 := n.Rect.Y
		w := n.Rect.W
		h := n.Rect.H
		if x0 < 0 {
			w += x0
			x0 = 0
		}
		if y0 < 0 {
			h += y0
			y0 = 0
		}
		if x0+w > bounds.W {
			w = bounds.W - x0
		}
		if y0+h > bounds.H {
			h = bounds.H - y0
		}
		if w <= 0 || h <= 0 {
			continue
		}

		paintNode(n, cells, bounds.W, x0, y0, w, h, st)
	}
	return core.Frame{Size: bounds, Cells: cells}
}

func paintNode(n *LayoutNode, cells []core.Cell, stride, x0, y0, w, h int, st StyleTable) {
	sid := n.Style
	if sid == 0 {
		// Default style.
		sid = st.allocate(core.RoleNone, core.StateNormal, Environment{})
	}

	base := y0 * stride
	col := x0

	// Indent by depth.
	indent := n.Depth
	if indent > w {
		indent = w
	}
	for i := 0; i < indent; i++ {
		if base+col < len(cells) {
			cells[base+col] = core.Cell{Rune: ' ', Style: sid}
		}
		col++
	}

	// Role prefix markers (v1 minimal).
	switch n.Kind {
	case "action":
		if col < x0+w && base+col < len(cells) {
			cells[base+col] = core.Cell{Rune: '>', Style: sid}
			col++
		}
	case "field":
		if col < x0+w && base+col < len(cells) {
			cells[base+col] = core.Cell{Rune: ':', Style: sid}
			col++
		}
	}

	// Label text with rune-width awareness.
	for _, r := range n.Label {
		if col >= x0+w {
			break
		}
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			// Zero-width runes (combining marks) are skipped in v1.
			continue
		}
		if base+col < len(cells) {
			if r == utf8.RuneError {
				cells[base+col] = core.Cell{Rune: '?', Style: sid}
			} else {
				cells[base+col] = core.Cell{Rune: r, Style: sid}
			}
		}
		col += rw
	}

	// Fill remainder of first row with spaces to clear stale cells.
	for col < x0+w && base+col < len(cells) {
		cells[base+col] = core.Cell{Rune: ' ', Style: sid}
		col++
	}
}

// ── Snapshot ─────────────────────────────────────────────────────────

// Snapshot is a serializable, deterministic summary of compiler output.
type Snapshot struct {
	VisibleLines []VisibleLine
	FocusGraph   FocusGraph
	FrameSize    core.Size
	FrameRows    []string
	Diagnostics  []core.Diagnostic
	StyleCount   int
}

// buildSnapshot builds a snapshot from compiler state.
func buildSnapshot[E any](norm normalized[E], out CompiledFrame[E]) Snapshot {
	rows := make([]string, 0, out.Frame.Size.H)
	rowCells := make([]rune, out.Frame.Size.W)
	for y := 0; y < out.Frame.Size.H; y++ {
		for x := 0; x < out.Frame.Size.W; x++ {
			idx := y*out.Frame.Size.W + x
			if idx < len(out.Frame.Cells) {
				r := out.Frame.Cells[idx].Rune
				if r == 0 {
					r = ' '
				}
				rowCells[x] = r
			} else {
				rowCells[x] = ' '
			}
		}
		rows = append(rows, string(rowCells))
	}

	var lines []VisibleLine
	for _, n := range out.LayoutTree.Nodes {
		lines = append(lines, VisibleLine{
			Offset: n.Offset,
			Key:    n.Key,
			Depth:  n.Depth,
			Label:  n.Label,
			Kind:   n.Kind,
		})
	}

	return Snapshot{
		VisibleLines: lines,
		FocusGraph:   out.FocusGraph,
		FrameSize:    out.Frame.Size,
		FrameRows:    rows,
		Diagnostics:  append([]core.Diagnostic(nil), out.Diagnostics.Items...),
		StyleCount:   len(out.StyleTable.defs),
	}
}

// VisibleLine is one derived terminal line from the layout tree.
type VisibleLine struct {
	Offset int
	Key    core.Key
	Depth  int
	Label  string
	Kind   string
}

//String returns a deterministic string for debugging.
func (v VisibleLine) String() string {
	return fmt.Sprintf("%d:%s[%s] %q", v.Depth, v.Key, v.Kind, v.Label)
}

// ── Empty helper ─────────────────────────────────────────────────────

// Empty returns an empty node for type inference convenience.
func Empty[E any]() core.Node[E] { return core.Empty[E]() }
