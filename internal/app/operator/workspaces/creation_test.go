package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/configstore"
)

func TestCreationPreviewSharesApplyValidationWithoutCredentialsOrEffects(t *testing.T) {
	for _, configured := range []bool{false, true} {
		service, _, path := testService(t)
		cmd := CreateWorkspace{Slug: "research", DisplayName: " Research "}
		if configured {
			cmd.InitialRoute, cmd.Target = "chat", openAITarget("fixture")
		}
		before, _ := os.ReadFile(path)
		admit := func(context.Context) error { return nil }
		preview, err := service.PreviewCreation(t.Context(), cmd, admit)
		if err != nil || preview.Slug != "research" || preview.DisplayName != "Research" || preview.ApprovalClass != "confirmation" {
			t.Fatal("invalid preview", err)
		}
		expectedCount := 0
		if configured {
			expectedCount = 1
		}
		if preview.RouteCount != expectedCount || preview.TargetCount != expectedCount {
			t.Fatal("preview changed route/target impact")
		}
		encoded, _ := json.Marshal(preview)
		if strings.Contains(string(encoded), "credential") || strings.Contains(string(encoded), "env:") || strings.Contains(string(encoded), "management_") {
			t.Fatal("preview exposed connection or fake authority")
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("preview published")
		}
		created, _, err := service.CreateWorkspaceChecked(t.Context(), cmd, "local", "key", admit)
		if err != nil || !reflect.DeepEqual(preview.Readiness, created.Readiness) || preview.RouteCount != len(created.Routes) {
			t.Fatal("preview/apply differ", err)
		}
	}
}

func TestCheckedCreationProjectsOriginalReceiptForEmptyAndConfiguredWorkspace(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "configured"}[configured], func(t *testing.T) {
			service, store, path := testService(t)
			cmd := CreateWorkspace{Slug: "research", DisplayName: " Research 東京 "}
			if configured {
				cmd.InitialRoute, cmd.Target = "chat", openAITarget("fixture")
			}
			admit := func(context.Context) error { return nil }
			original, replay, err := service.CreateWorkspaceChecked(t.Context(), cmd, "local", "key", admit)
			if err != nil || replay || original.DisplayName != "Research 東京" || original.ManagementETag == "" {
				t.Fatal("invalid committed result", err)
			}
			current, err := service.GetWorkspace(t.Context(), "research")
			if err != nil || !reflect.DeepEqual(current, original) {
				t.Fatal("creation/detail disagree", err)
			}
			if _, err := service.SetDisplayNameChecked(t.Context(), "research", "Later", original.ManagementETag, admit); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = configstore.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			service, err = NewService(store, Dependencies{})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			got, replay, err := service.CreateWorkspaceChecked(t.Context(), cmd, "local", "key", admit)
			if err != nil || !replay || !reflect.DeepEqual(got, original) {
				t.Fatal("replay returned current state", err)
			}
			cmd.DisplayName = "Changed"
			if _, _, err := service.CreateWorkspaceChecked(t.Context(), cmd, "local", "key", admit); !errors.Is(err, configstore.ErrIdempotencyConflict) {
				t.Fatal("changed body did not preserve conflict", err)
			}
			if configured {
				changed := cmd
				changed.DisplayName = original.DisplayName
				changed.Target = openAITarget("another-model")
				if _, _, err := service.CreateWorkspaceChecked(t.Context(), changed, "local", "key", admit); !errors.Is(err, configstore.ErrIdempotencyConflict) {
					t.Fatal("changed configured target did not conflict", err)
				}
			}
			revoked := errors.New("admission revoked")
			cmd.DisplayName = original.DisplayName
			if _, _, err := service.CreateWorkspaceChecked(t.Context(), cmd, "local", "key", func(context.Context) error { return revoked }); !errors.Is(err, revoked) {
				t.Fatal("admission error lost", err)
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("replay/conflict published")
			}
		})
	}
}

func TestCheckedCreationInvalidIntentHasNoPartialEffect(t *testing.T) {
	service, _, path := testService(t)
	before, _ := os.ReadFile(path)
	for _, cmd := range []CreateWorkspace{{Slug: "invalid", DisplayName: "bad\nname"}, {Slug: "invalid", InitialRoute: "chat", Target: TargetDraft{}}} {
		called := false
		if _, _, err := service.CreateWorkspaceChecked(t.Context(), cmd, "local", "key", func(context.Context) error { called = true; return nil }); err == nil {
			t.Fatal("invalid intent accepted")
		}
		if called {
			t.Fatal("invalid intent reached publication admission")
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid creation published")
	}
}
