package fixture

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/testkit/testscreen"
)

func TestCanonicalANSIFixtureCorpus(t *testing.T) {
	root := filepath.Clean("../../../cockpit")
	var count int
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".ansi" {
			return nil
		}
		count++
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		screen, err := testscreen.ParseANSI(raw)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		canonical := testscreen.WriteANSI(screen)
		exact := testscreen.WriteANSIExact(screen)
		if !bytes.Equal(canonical, raw) && !bytes.Equal(exact, raw) {
			t.Errorf("noncanonical ANSI: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no canonical ANSI visual fixtures found")
	}
}

func TestVisualFixtureCorpusHasNoTextSiblings(t *testing.T) {
	root := filepath.Clean("../../../cockpit")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".txt" || !strings.Contains(filepath.ToSlash(path), "/fixture/") {
			return nil
		}
		if _, err := os.Stat(strings.TrimSuffix(path, ".txt") + ".ansi"); err == nil {
			t.Errorf("duplicate visual fixture formats: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
