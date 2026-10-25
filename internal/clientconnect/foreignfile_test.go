package clientconnect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Connect edits the user's client configuration, not Swobu's security policy.
// Existing mode/ACL remains the host operator's choice even with a credential.
func TestForeignFileReplacePreservesOperatorSelectedMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	before := []byte(`{"apiKey":"before"}`)
	after := []byte(`{"apiKey":"after"}`)
	if err := os.WriteFile(path, before, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	file, err := inspectForeignFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.replace(after); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(after) {
		t.Fatalf("replacement=%s error=%v", actual, err)
	}
	backup, err := os.ReadFile(path + ".swobu.bak")
	if err != nil || string(backup) != string(before) {
		t.Fatalf("backup=%s error=%v", backup, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0644 {
			t.Fatalf("existing permissions changed: %v %v", info, err)
		}
	}
}

func TestNewForeignFileUsesRestrictiveMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.json")
	file, err := inspectForeignFile(path, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.replace([]byte(`{"apiKey":"issued"}`)); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("new file mode wrong: %v %v", info, err)
		}
	}
}
