package clientconnect

type keyPath []string

// stringDocumentEditor owns format mechanics for one string-valued key path.
// Client selection, admission, and path meaning remain adapter concerns.
type stringDocumentEditor interface {
	String([]byte, keyPath) (string, bool, error)
	SetString([]byte, keyPath, string) ([]byte, error)
}
