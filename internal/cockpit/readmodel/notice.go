package readmodel

// NoticeKind classifies bounded operator-facing shell notices.
type NoticeKind int

const (
	NoticeNone NoticeKind = iota
	NoticeInfo
	NoticeWarning
	NoticeError
	NoticeStale
)

// Notice is a typed, bounded user-visible message.
//
// Message is display copy. Kind is behavior or styling state; callers should
// not infer semantics from substrings in Message.
type Notice struct {
	Kind    NoticeKind
	Message string
}

// IsEmpty reports whether there is no notice to render.
func (n Notice) IsEmpty() bool {
	return n.Kind == NoticeNone && n.Message == ""
}

// Visible reports whether the notice has user-visible copy.
func (n Notice) Visible() bool {
	return n.Message != ""
}
