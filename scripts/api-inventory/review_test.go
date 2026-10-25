package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Every source mount must have an explicit reviewed dispatch contract. The
// generated inventory catches source changes; this table forces ownership
// reconciliation when a mount is added or removed instead of refreshing blindly.
func TestEveryMountHasReviewedDispatchOwner(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "api", "mounted-route-review.json"))
	if err != nil {
		t.Fatal(err)
	}
	var review struct {
		Groups map[string]struct {
			Source        string         `json:"dispatch_source"`
			Contract      string         `json:"method_contract"`
			Registrations []registration `json:"registrations"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &review); err != nil {
		t.Fatal(err)
	}
	value, err := collect(root)
	if err != nil {
		t.Fatal(err)
	}
	key := func(r registration) string {
		return r.Source + "\x00" + r.Function + "\x00" + r.Expression + "\x00" + r.Owner
	}
	remaining := map[string]int{}
	for _, r := range value.Registrations {
		remaining[key(r)]++
	}
	for name, group := range review.Groups {
		if group.Contract == "" || len(group.Registrations) == 0 {
			t.Errorf("empty review %s", name)
		}
		if _, err := os.Stat(filepath.Join(root, group.Source)); err != nil {
			t.Errorf("dispatch owner %s: %v", name, err)
		}
		for _, r := range group.Registrations {
			k := key(r)
			if remaining[k] == 0 {
				t.Errorf("unmounted/duplicate review %s: %s", name, r.Expression)
				continue
			}
			remaining[k]--
		}
	}
	for k, count := range remaining {
		if count != 0 {
			t.Errorf("mount without dispatch review: %s", k)
		}
	}
}
