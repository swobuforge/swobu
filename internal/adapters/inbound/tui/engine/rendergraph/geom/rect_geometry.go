package geom

// Point is one integer position in terminal cell space.
type Point struct {
	X int
	Y int
}

// Size is one width/height pair in terminal cells.
type Size struct {
	W int
	H int
}

// Rect is one axis-aligned integer box in terminal cell space.
type Rect struct {
	X int
	Y int
	W int
	H int
}

func (r Rect) Right() int  { return r.X + r.W }
func (r Rect) Bottom() int { return r.Y + r.H }

func (r Rect) Empty() bool {
	return r.W <= 0 || r.H <= 0
}

func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X < r.Right() &&
		p.Y >= r.Y && p.Y < r.Bottom()
}

func (r Rect) Intersect(o Rect) Rect {
	x1 := max(r.X, o.X)
	y1 := max(r.Y, o.Y)
	x2 := min(r.Right(), o.Right())
	y2 := min(r.Bottom(), o.Bottom())
	if x2 <= x1 || y2 <= y1 {
		return Rect{}
	}
	return Rect{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

func (r Rect) Translate(p Point) Rect {
	return Rect{X: r.X + p.X, Y: r.Y + p.Y, W: r.W, H: r.H}
}

// Insets shrink a parent box into a child content box.
type Insets struct {
	Top    int
	Right  int
	Bottom int
	Left   int
}

func (in Insets) Horizontal() int { return in.Left + in.Right }
func (in Insets) Vertical() int   { return in.Top + in.Bottom }

func (in Insets) Apply(r Rect) Rect {
	return Rect{
		X: r.X + in.Left,
		Y: r.Y + in.Top,
		W: max(0, r.W-in.Horizontal()),
		H: max(0, r.H-in.Vertical()),
	}
}

// AxisConstraint constrains one axis; Max == -1 means unbounded.
type AxisConstraint struct {
	Min int
	Max int
}

// Constraints are per-axis min/max bounds for measurement.
type Constraints struct {
	W AxisConstraint
	H AxisConstraint
}

func Exact(w, h int) Constraints {
	return Constraints{
		W: AxisConstraint{Min: w, Max: w},
		H: AxisConstraint{Min: h, Max: h},
	}
}

func AtMost(w, h int) Constraints {
	return Constraints{
		W: AxisConstraint{Min: 0, Max: w},
		H: AxisConstraint{Min: 0, Max: h},
	}
}

func Unbounded() Constraints {
	return Constraints{
		W: AxisConstraint{Min: 0, Max: -1},
		H: AxisConstraint{Min: 0, Max: -1},
	}
}

func ClampSize(s Size, c Constraints) Size {
	return Size{
		W: clampAxis(s.W, c.W),
		H: clampAxis(s.H, c.H),
	}
}

func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clampAxis(v int, c AxisConstraint) int {
	if v < c.Min {
		v = c.Min
	}
	if c.Max >= 0 && v > c.Max {
		v = c.Max
	}
	return v
}
