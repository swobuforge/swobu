package view

// WithPadding applies explicit edge insets.
func WithPadding[M any](top, right, bottom, left int) func(ViewSpec[M]) ViewSpec[M] {
	return func(base ViewSpec[M]) ViewSpec[M] {
		return Padded(base, top, right, bottom, left)
	}
}

// WithInsets applies a typed inset record.
func WithInsets[M any](in Insets) func(ViewSpec[M]) ViewSpec[M] {
	return func(base ViewSpec[M]) ViewSpec[M] {
		return Inset(base, in)
	}
}

// WithPadLeft applies left inset only.
func WithPadLeft[M any](left int) func(ViewSpec[M]) ViewSpec[M] {
	return func(base ViewSpec[M]) ViewSpec[M] {
		return Padded(base, 0, 0, 0, left)
	}
}

// WithPadRight applies right inset only.
func WithPadRight[M any](right int) func(ViewSpec[M]) ViewSpec[M] {
	return func(base ViewSpec[M]) ViewSpec[M] {
		return Padded(base, 0, right, 0, 0)
	}
}

// WithPadX applies horizontal insets.
func WithPadX[M any](left, right int) func(ViewSpec[M]) ViewSpec[M] {
	return func(base ViewSpec[M]) ViewSpec[M] {
		return Padded(base, 0, right, 0, left)
	}
}

// WithPadY applies vertical insets.
func WithPadY[M any](top, bottom int) func(ViewSpec[M]) ViewSpec[M] {
	return func(base ViewSpec[M]) ViewSpec[M] {
		return Padded(base, top, 0, bottom, 0)
	}
}

// WithGrow marks the view as consuming remaining flow space.
func WithGrow[M any]() func(ViewSpec[M]) ViewSpec[M] {
	return func(base ViewSpec[M]) ViewSpec[M] {
		return Grow(base)
	}
}

// WithConstrain applies a sizing/constraint envelope.
func WithConstrain[M any](spec ConstrainSpec) func(ViewSpec[M]) ViewSpec[M] {
	return func(base ViewSpec[M]) ViewSpec[M] {
		return Constrain(base, spec)
	}
}

// WithMaxWidth constrains maximum width.
func WithMaxWidth[M any](maxW int) func(ViewSpec[M]) ViewSpec[M] {
	return WithConstrain[M](ConstrainSpec{GrowW: true, MaxW: maxW})
}

// WithFixedSize constrains to fixed width and height.
func WithFixedSize[M any](w, h int) func(ViewSpec[M]) ViewSpec[M] {
	return WithConstrain[M](ConstrainSpec{FixedW: w, FixedH: h})
}

// WithScrollY wraps the subtree in a vertical viewport at offset.
func WithScrollY[M any](offset int) func(ViewSpec[M]) ViewSpec[M] {
	return func(base ViewSpec[M]) ViewSpec[M] {
		return ScrollY(base, offset)
	}
}
