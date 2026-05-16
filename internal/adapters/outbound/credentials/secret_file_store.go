package credentials

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

type secretFileStore struct {
	mu sync.Mutex
}

type secretFileDoc struct {
	Credentials map[string]string `json:"credentials"`
}

func (s *secretFileStore) Store(keyName string, secret string) error {
	name := strings.TrimSpace(keyName) // trimlowerlint:allow boundary canonicalization
	token := strings.TrimSpace(secret) // trimlowerlint:allow boundary canonicalization
	if name == "" {
		return fmt.Errorf("credential file key name is required")
	}
	if token == "" {
		return fmt.Errorf("credential file key value is required")
	}
	path := secretFilePath()
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := readSecretFileDoc(path)
	if err != nil {
		return err
	}
	if doc.Credentials == nil {
		doc.Credentials = map[string]string{}
	}
	doc.Credentials[name] = token
	return writeSecretFileDoc(path, doc)
}

func (s *secretFileStore) Resolve(keyName string) (string, error) {
	name := strings.TrimSpace(keyName) // trimlowerlint:allow boundary canonicalization
	if name == "" {
		return "", fmt.Errorf("credential file key name is required")
	}
	path := secretFilePath()
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := readSecretFileDoc(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(doc.Credentials[name]) // trimlowerlint:allow boundary canonicalization
	if token == "" {
		return "", fmt.Errorf("credential file token for %q is empty or missing", name)
	}
	return token, nil
}

func secretFilePath() string {
	return platformconfig.DefaultAuthCredentialFilePath()
}

func readSecretFileDoc(path string) (secretFileDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return secretFileDoc{Credentials: map[string]string{}}, nil
		}
		return secretFileDoc{}, fmt.Errorf("credential file read failed: %w", err)
	}
	if strings.TrimSpace(string(raw)) == "" { // trimlowerlint:allow boundary canonicalization
		return secretFileDoc{Credentials: map[string]string{}}, nil
	}
	var doc secretFileDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return secretFileDoc{}, fmt.Errorf("credential file decode failed: %w", err)
	}
	if doc.Credentials == nil {
		doc.Credentials = map[string]string{}
	}
	return doc, nil
}

func writeSecretFileDoc(path string, doc secretFileDoc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("credential file dir create failed: %w", err)
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("credential file encode failed: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("credential file write failed: %w", err)
	}
	return nil
}
