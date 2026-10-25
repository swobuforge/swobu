// Package credentialaudience owns the closed purpose of an authentication
// credential. Audience is independent of Principal identity and never implies
// that a grant exists; current permission checks remain admission-owned.
package credentialaudience

// Kind distinguishes Workspace data from remote Node administration. Local
// browser/installation authority is not a Principal credential audience.
type Kind uint8

const (
	WorkspaceData Kind = iota + 1
	NodeAdministration
)

// Audience is constructed explicitly. Its zero value grants no purpose and a
// Workspace audience cannot match Node administration, including for empty IDs.
type Audience struct {
	kind      Kind
	workspace string
}

func Workspace(slug string) Audience {
	if slug == "" {
		return Audience{}
	}
	return Audience{kind: WorkspaceData, workspace: slug}
}

func Node() Audience          { return Audience{kind: NodeAdministration} }
func (a Audience) Kind() Kind { return a.kind }
func (a Audience) AllowsWorkspace(slug string) bool {
	return a.kind == WorkspaceData && slug != "" && a.workspace == slug
}
func (a Audience) AllowsNodeAdministration() bool { return a.kind == NodeAdministration }
