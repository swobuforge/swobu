package clientconnect

// OtherClientSetupPrompt returns a secret-free setup instruction for clients
// that are not automatically writable by the local Connect service.
func OtherClientSetupPrompt(target Target) string {
	return "Set the client base URL to " + target.WorkspaceURL() + " and use the Workspace route as the model."
}
