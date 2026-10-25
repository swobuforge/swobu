// Package adminmcp adapts explicitly mounted builtin tools to canonical Node
// Control queries and checked commands. Workspace ingress supplies live caller
// admission; tools cannot mint local authority or accept a caller identity in
// their arguments. Secrets and transport credentials never enter this package.
package adminmcp
