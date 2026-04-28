package deprecated

// DeprecatedFunc: Deprecated, use NewFunc instead. // want "use of 'deprecated' in comments is forbidden; remove obsoleted code instead of annotating it"
func DeprecatedFunc() {}

// NewFunc is the replacement.
func NewFunc() {}

// DeprecatedType is Deprecated and should not be used. // want "use of 'deprecated' in comments is forbidden; remove obsoleted code instead of annotating it"
type DeprecatedType struct{} // want `no-method struct "DeprecatedType" must use a data suffix`

// ReplacementType is the replacement.
type ReplacementType struct{} // want `no-method struct "ReplacementType" must use a data suffix`

// inline comment with deprecated word // want "use of 'deprecated' in comments is forbidden; remove obsoleted code instead of annotating it"
func WithInlineComment() {}

/* DEPRECATED: block comment with uppercase */ // want "use of 'deprecated' in comments is forbidden; remove obsoleted code instead of annotating it"
func WithBlockDeprecated()                     {}

// this function is casual mention of deprecated // want "use of 'deprecated' in comments is forbidden; remove obsoleted code instead of annotating it"
func WithCasualDeprecated() {}

// this is fine — no bad words here
func CleanFunction() {}
