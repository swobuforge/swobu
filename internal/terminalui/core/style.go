package core

// StyleToken is the semantic style token resolved by render adapters.
type StyleToken string

const (
	TokenTextDefault     StyleToken = "text.default"
	TokenTextMuted       StyleToken = "text.muted"
	TokenTextDanger      StyleToken = "text.danger"
	TokenTextSuccess     StyleToken = "text.success"
	TokenAccentPrimary   StyleToken = "accent.primary"
	TokenSurfaceDefault  StyleToken = "surface.default"
	TokenSurfaceSelected StyleToken = "surface.selected"
	TokenBorderDefault   StyleToken = "border.default"
	TokenBorderFocused   StyleToken = "border.focused"
)

// Variant is a free-form style variant selector.
type Variant string

// Tri is a three-valued switch for style modifiers.
type Tri uint8

const (
	TriUnset Tri = iota
	TriFalse
	TriTrue
)

// StyleMods captures orthogonal visual modifiers.
type StyleMods struct {
	Bold      Tri
	Dim       Tri
	Underline Tri
}

// VisualState describes the semantic state of one node.
type VisualState uint8

const (
	StateDefault VisualState = iota
	StateFocused
	StateSelected
	StateDisabled
	StateDanger
	StateWarning
	StateSuccess
	StateLoading
	StateStale
)

// Style is the semantic styling envelope for one node.
type Style struct {
	Token   StyleToken
	Variant Variant
	State   VisualState
	Mods    StyleMods
}
