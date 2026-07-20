package canonical

// Specified records whether a request band was supplied independently of its
// value. It owns omission only; the contained canonical value owns validation
// and cloning.
type Specified[T any] struct {
	value T
	set   bool
}

// Unspecified constructs an omitted request band.
func Unspecified[T any]() Specified[T] {
	return Specified[T]{}
}

// Specify constructs an explicitly supplied request band, including an
// explicitly empty value.
func Specify[T any](value T) Specified[T] {
	return Specified[T]{value: value, set: true}
}

// Get returns the stored value and whether the band was supplied.
func (s Specified[T]) Get() (T, bool) {
	return s.value, s.set
}

// IsSpecified reports whether the band was supplied.
func (s Specified[T]) IsSpecified() bool {
	return s.set
}
