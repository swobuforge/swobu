package credentialfiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowseExpandsCanonicalHomeRelativePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	listing, err := Browse("~/")
	if err != nil {
		t.Fatal(err)
	}
	if listing.Path != filepath.Clean(home) {
		t.Fatalf("path = %q, want %q", listing.Path, filepath.Clean(home))
	}
}

func TestBrowseRejectsWorkingDirectoryRelativePath(t *testing.T) {
	_, err := Browse("relative/provider.key")
	if err == nil || err.Error() != "credential file path must be absolute or home-relative" {
		t.Fatalf("error = %v", err)
	}
}

func TestBrowseConstructsDaemonOwnedNavigationFacts(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, ".credentials")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(child, "provider.key")
	if err := os.WriteFile(file, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	listing, err := Browse(file)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Path != child || listing.Parent != dir {
		t.Fatalf("listing = %#v", listing)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Path != file || listing.Entries[0].IsDir {
		t.Fatalf("entries = %#v", listing.Entries)
	}
}

func TestBrowseMissingFileUsesNearestExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	listing, err := Browse(filepath.Join(dir, "missing", "provider.key"))
	if err != nil {
		t.Fatal(err)
	}
	if listing.Path != dir {
		t.Fatalf("path = %q, want %q", listing.Path, dir)
	}
}
