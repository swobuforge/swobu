package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)authorization`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{16,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\bsk-or-v1-[A-Za-z0-9_-]{16,}\b`),
}

func main() {
	var (
		providerPath = flag.String("provider-path", "test/fixtures/live_matrix/records", "path to provider live evidence artifacts")
		clientPath   = flag.String("client-path", "test/artifacts/live/client_integration", "path to client integration live evidence artifacts")
	)
	flag.Parse()

	paths := []string{strings.TrimSpace(*providerPath), strings.TrimSpace(*clientPath)}
	var findings []string
	for _, root := range paths {
		if root == "" {
			continue
		}
		found, err := scanPath(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: scan %s: %v\n", root, err)
			os.Exit(1)
		}
		findings = append(findings, found...)
	}
	if len(findings) == 0 {
		fmt.Println("secret redaction checks passed")
		return
	}
	for _, finding := range findings {
		fmt.Fprintf(os.Stderr, "error: %s\n", finding)
	}
	os.Exit(1)
}

func scanPath(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{fmt.Sprintf("missing evidence path %s", root)}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return scanFile(root)
	}
	var findings []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".json", ".txt", ".log", ".md", ".yaml", ".yml":
		default:
			return nil
		}
		fileFindings, err := scanFile(path)
		if err != nil {
			return err
		}
		findings = append(findings, fileFindings...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

func scanFile(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !isText(raw) {
		return nil, nil
	}
	var findings []string
	lines := bytes.Split(raw, []byte{'\n'})
	for i, lineRaw := range lines {
		lineNo := i + 1
		line := string(lineRaw)
		for _, pattern := range secretPatterns {
			if pattern.MatchString(line) {
				findings = append(findings, fmt.Sprintf("%s:%d matched %q", path, lineNo, pattern.String()))
			}
		}
	}
	return findings, nil
}

func isText(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	if bytes.Contains(raw, []byte{0x00}) {
		return false
	}
	return true
}
