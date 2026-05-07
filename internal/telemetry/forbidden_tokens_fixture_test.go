package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type forbiddenTokenFixture struct {
	ForbiddenTokens []string `json:"forbidden_tokens"`
}

func loadForbiddenTokensFixture(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "test", "contracts", "telemetry", "forbidden_tokens_v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read forbidden token fixture: %v", err)
	}
	var fixture forbiddenTokenFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode forbidden token fixture: %v", err)
	}
	if len(fixture.ForbiddenTokens) == 0 {
		t.Fatal("forbidden token fixture is empty")
	}
	return fixture.ForbiddenTokens
}
