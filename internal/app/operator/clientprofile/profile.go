package clientprofile

// Profile defines one client integration profile.
//
// Static identity is returned by Identity; runtime-derived operator actions are
// returned by Actions.
type Profile interface {
	Identity() Identity
	Actions(baseURL string) []Action
}
