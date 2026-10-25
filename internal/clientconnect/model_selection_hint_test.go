package clientconnect

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEndpointOnlyAdaptersDiscloseExplicitModelSelection(t *testing.T) {
	for _, client := range []ClientID{ClientClaude, ClientAntigravity} {
		t.Run(string(client), func(t *testing.T) {
			var mutation plannedMutation
			var err error
			file := filepath.Join(t.TempDir(), "settings.json")
			if client == ClientClaude {
				mutation, err = planClaude(file, testTarget(t))
			} else {
				mutation, err = planAntigravity(file, testTarget(t), antigravityEnvironmentPlan{})
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(mutation.plan.PostApplyHint, "coding") || !strings.Contains(mutation.plan.PostApplyHint, "explicitly") {
				t.Fatal("endpoint-only review omitted required client model selection")
			}
			for _, change := range mutation.plan.Changes {
				if change.Field == "model" {
					t.Fatal("endpoint-only adapter claimed model pinning")
				}
			}
		})
	}
}
