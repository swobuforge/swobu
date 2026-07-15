package clientprofile

// Profile is the static profile contract for launcher/capability declarations.
//
// Static identity is returned by Identity; runtime-derived operator actions are
// returned by Actions.
type Profile interface {
	Identity() Identity
	Actions(baseURL string) []Action
}
