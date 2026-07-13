// Package inspector provides a dev-mode diagnostic overlay for core.Node trees.
// It is gated behind dev mode and must never reach production codepaths.
package inspector

import "os"

// Enabled reports whether the inspector is active for this process.
// It checks the SWOBU_INSPECTOR environment variable.
func Enabled() bool {
	return os.Getenv("SWOBU_INSPECTOR") != ""
}

// CurrentMode returns the active inspector mode from the environment.
// Defaults to ModeDiagnostics when SWOBU_INSPECTOR is set but empty,
// or to the value of SWOBU_INSPECTOR if it is one of the known modes.
func CurrentMode() Mode {
	env := os.Getenv("SWOBU_INSPECTOR")
	switch env {
	case "":
		return ModeDiagnostics
	case string(ModeLayout):
		return ModeLayout
	case string(ModeFocus):
		return ModeFocus
	default:
		return ModeDiagnostics
	}
}
