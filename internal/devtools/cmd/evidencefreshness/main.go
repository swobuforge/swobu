package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type freshnessTargetSpec struct {
	label string
	path  string
}

func main() {
	var (
		maxAge  = flag.Duration("max-age", 14*24*time.Hour, "maximum allowed artifact age")
		records = flag.String("records", "test/fixtures/live_matrix/records", "path to provider live evidence records directory")
		client  = flag.String("client-artifact", "test/artifacts/live/client_integration/openrouter_latest.json", "path to latest client integration evidence artifact")
	)
	flag.Parse()

	now := time.Now()
	targets := []freshnessTargetSpec{
		{label: "provider live records", path: strings.TrimSpace(*records)},
		{label: "client live evidence", path: strings.TrimSpace(*client)},
	}

	var failed bool
	for _, check := range targets {
		latest, err := newestTimestamp(check.path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s (%s): %v\n", check.label, check.path, err)
			failed = true
			continue
		}
		age := now.Sub(latest)
		if age > *maxAge {
			fmt.Fprintf(
				os.Stderr,
				"error: %s stale: age=%s max_age=%s latest=%s path=%s\n",
				check.label,
				age.Truncate(time.Second),
				maxAge.String(),
				latest.UTC().Format(time.RFC3339),
				check.path,
			)
			failed = true
			continue
		}
		fmt.Printf(
			"fresh: %s age=%s latest=%s path=%s\n",
			check.label,
			age.Truncate(time.Second),
			latest.UTC().Format(time.RFC3339),
			check.path,
		)
	}

	if failed {
		os.Exit(1)
	}
}

func newestTimestamp(path string) (time.Time, error) {
	if strings.TrimSpace(path) == "" {
		return time.Time{}, errors.New("path must not be empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	if !info.IsDir() {
		return info.ModTime(), nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return time.Time{}, err
	}
	var newest time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".json" && ext != ".txt" && ext != ".log" {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return time.Time{}, err
		}
		if entryInfo.ModTime().After(newest) {
			newest = entryInfo.ModTime()
		}
	}
	if newest.IsZero() {
		return time.Time{}, fmt.Errorf("no evidence files found")
	}
	return newest, nil
}
