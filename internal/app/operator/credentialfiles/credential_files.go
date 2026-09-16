// Package credentialfiles owns credential-file enumeration in the daemon's
// filesystem namespace.
package credentialfiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent,omitempty"`
	Entries []Entry `json:"entries"`
}

type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

func Browse(path string) (Listing, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Listing{}, err
	}
	if path == "" {
		path = home
	} else if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	} else if !filepath.IsAbs(path) {
		return Listing{}, errors.New("credential file path must be absolute or home-relative")
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	} else if errors.Is(err, os.ErrNotExist) {
		path, err = nearestExistingDirectory(path)
	}
	if err != nil {
		return Listing{}, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Listing{}, err
	}
	listing := Listing{Path: path, Entries: make([]Entry, 0, len(entries))}
	if parent := filepath.Dir(path); parent != path {
		listing.Parent = parent
	}
	for _, entry := range entries {
		listing.Entries = append(listing.Entries, Entry{Name: entry.Name(), Path: filepath.Join(path, entry.Name()), IsDir: entry.IsDir()})
	}
	return listing, nil
}

func nearestExistingDirectory(path string) (string, error) {
	for {
		parent := filepath.Dir(path)
		info, err := os.Stat(parent)
		if err == nil && info.IsDir() {
			return parent, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if parent == path {
			return "", os.ErrNotExist
		}
		path = parent
	}
}
