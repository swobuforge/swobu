package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseBuildRejectsInvalidIdentityBeforeOutput(t *testing.T) {
	for _, arguments := range [][]string{
		{"2.0.1", strings.Repeat("a", 40), strings.Repeat("b", 40)},
		{"v2.0.1", "HEAD", strings.Repeat("b", 40)},
		{"v2.0.1", strings.Repeat("a", 40), "../private"},
		{"v2.0.1;exit", strings.Repeat("a", 40), strings.Repeat("b", 40)},
		{"v2.0.1\nother", strings.Repeat("a", 40), strings.Repeat("b", 40)},
		{"v2.0.1", strings.Repeat("a", 40) + "\nother", strings.Repeat("b", 40)},
		{"v2.0.1", strings.Repeat("a", 40), strings.Repeat("b", 40) + "\n"},
	} {
		directory := t.TempDir()
		output := filepath.Join(directory, "release")
		probe := filepath.Join(directory, "called")
		if err := os.WriteFile(filepath.Join(directory, "go"), []byte("#!/bin/sh\ntouch \"$PROBE_OUTPUT\"\nexit 1\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		command := exec.Command("sh", append([]string{"../build-release.sh"}, append(arguments, output)...)...)
		command.Env = append(os.Environ(), "PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"), "PROBE_OUTPUT="+probe)
		if err := command.Run(); err == nil {
			t.Fatalf("accepted invalid release arguments %q", arguments)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid release created output")
		}
		if _, err := os.Stat(probe); !os.IsNotExist(err) {
			t.Errorf("invalid release arguments reached toolchain: %q", arguments)
		}
	}
}

func TestReleaseBuildControlsFIPSSelection(t *testing.T) {
	directory := t.TempDir()
	probe := filepath.Join(directory, "environment")
	tool := "#!/bin/sh\nprintf '%s' \"$GOFIPS140\" > \"$PROBE_OUTPUT\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(directory, "go"), []byte(tool), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "../build-release.sh", "v2.0.1",
		strings.Repeat("a", 40), strings.Repeat("b", 40), filepath.Join(directory, "release"))
	command.Env = append(os.Environ(), "PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GOFIPS140=latest", "PROBE_OUTPUT="+probe)
	if err := command.Run(); err == nil {
		t.Fatal("probe toolchain should stop before building")
	}
	actual, err := os.ReadFile(probe)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "off" {
		t.Fatalf("release inherited FIPS selection %q, want off", actual)
	}
}
