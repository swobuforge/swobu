package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestInstallerSeparatesReleaseTagAndProductVersion(t *testing.T) {
	command := exec.Command("sh", "../install.sh", "--version", "swobu-v2.0.1", "--dry-run")
	command.Env = append(os.Environ(), "START_SWOBU=false")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("installer: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "tag=swobu-v2.0.1\n") ||
		!strings.Contains(text, "archive=swobu_v2.0.1_") ||
		!strings.Contains(text, "/releases/download/swobu-v2.0.1/swobu_v2.0.1_") {
		t.Fatalf("tag and product version were conflated:\n%s", text)
	}
}

func TestPowerShellInstallerSeparatesReleaseTag(t *testing.T) {
	powershell, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell is not installed")
	}
	command := exec.Command(powershell, "-NoProfile", "-File", "../install.ps1",
		"-Version", "swobu-v2.0.1", "-DryRun", "-NoStart")
	command.Env = append(os.Environ(), "LOCALAPPDATA="+t.TempDir())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("PowerShell installer: %v\n%s", err, output)
	}
	text := strings.ReplaceAll(string(output), "\r\n", "\n")
	if !strings.Contains(text, "tag=swobu-v2.0.1\n") ||
		!strings.Contains(text, "archive=swobu_v2.0.1_windows_") ||
		!strings.Contains(text, "/releases/download/swobu-v2.0.1/swobu_v2.0.1_windows_") {
		t.Fatalf("tag and product version were conflated:\n%s", text)
	}
}
