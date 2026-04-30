package invariants

import (
	"os/exec"
	"strings"
	"testing"
)

func TestInstallScriptParsesAndDryRunsPinnedVersion(t *testing.T) {
	t.Parallel()

	script := repoPath(t, "swobucli", "scripts", "install.sh")
	cmd := exec.Command("sh", script, "--dry-run", "--version", "v0.0.0-test", "--bin-dir", "/tmp/swobu-bin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install script dry-run failed: %v\n%s", err, string(out))
	}

	text := string(out)
	required := []string{
		"tag=v0.0.0-test",
		"archive=swobu_v0.0.0-test_",
		"checksums_url=https://github.com/metrofun/swobu/releases/download/v0.0.0-test/checksums.txt",
		"install_dir=/tmp/swobu-bin",
	}
	for _, item := range required {
		if !strings.Contains(text, item) {
			t.Fatalf("dry-run output missing %q:\n%s", item, text)
		}
	}
}

func TestInstallScriptRejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	script := repoPath(t, "swobucli", "scripts", "install.sh")
	cmd := exec.Command("sh", script, "--unknown-flag")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure for unknown flag, got success:\n%s", string(out))
	}
	if !strings.Contains(string(out), "unknown argument") {
		t.Fatalf("unexpected error output:\n%s", string(out))
	}
}
