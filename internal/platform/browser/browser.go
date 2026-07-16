// Package browser opens URLs in the system default browser.
package browser

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Open starts the default browser for the given raw URL.
// It returns an error if the URL is empty or the platform command fails.
func Open(raw string) error {
	url := strings.TrimSpace(raw)
	if url == "" {
		return fmt.Errorf("browser open: URL is missing")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
