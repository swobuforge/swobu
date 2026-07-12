package core

// Capability names the external authority a node or effect requires.
type Capability string

const (
	CapabilityClipboard  Capability = "clipboard"
	CapabilityBrowser    Capability = "browser.open"
	CapabilityFilesystem Capability = "filesystem"
	CapabilityNetwork    Capability = "network"
	CapabilityShell      Capability = "shell"
	CapabilityForeground Capability = "foreground"
)
