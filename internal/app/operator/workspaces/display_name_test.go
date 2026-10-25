package workspaces

import (
	"context"

	"strings"
	"testing"
)

func TestWorkspaceCreationPublishesDisplayNameAtomically(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "configured"}[configured], func(t *testing.T) {
			service, _, _ := testService(t)
			command := CreateWorkspace{Slug: "research", DisplayName: "  Research 東京  "}
			if configured {
				command.InitialRoute, command.Target = "chat", openAITarget("fixture")
			}
			created, _, err := service.CreateWorkspaceChecked(t.Context(), command, "fixture-operator", "fixture-1752", func(context.Context) error { return nil })
			if err != nil || created.DisplayName != "Research 東京" || created.Slug != "research" {
				t.Fatal("creation lost display metadata", err)
			}
			read, err := service.GetWorkspace(t.Context(), "research")
			if err != nil || read.DisplayName != created.DisplayName || read.ManagementETag != created.ManagementETag {
				t.Fatal("creation/detail disagree")
			}
			for _, name := range []string{"bad\nname", strings.Repeat("界", 129)} {
				command.Slug, command.DisplayName = "invalid", name
				if _, _, err := service.CreateWorkspaceChecked(t.Context(), command, "fixture-operator", "fixture-2308", func(context.Context) error { return nil }); err == nil {
					t.Fatal("invalid name accepted")
				}
				if _, err := service.GetWorkspace(t.Context(), "invalid"); err == nil {
					t.Fatal("invalid metadata left partial Workspace")
				}
			}
		})
	}
}

func TestCheckedDisplayNameProjectsIdentityWithoutRename(t *testing.T) {
	service, _, _ := testService(t)
	created, _, err := service.CreateWorkspaceChecked(t.Context(), CreateWorkspace{Slug: "research"}, "fixture-operator", "fixture-2689", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.SetDisplayNameChecked(t.Context(), "research", "Research Team", created.ManagementETag, func(context.Context) error { return nil })
	if err != nil || updated.DisplayName != "Research Team" || updated.Slug != created.Slug || updated.ManagementETag == created.ManagementETag {
		t.Fatal("metadata edit lost locked projection", err)
	}
	read, err := service.GetWorkspace(t.Context(), "research")
	if err != nil || read.DisplayName != updated.DisplayName || read.ManagementETag != updated.ManagementETag {
		t.Fatal("detail lost metadata")
	}
	listed, err := service.ListWorkspaces(t.Context())
	if err != nil || len(listed) != 1 || listed[0].DisplayName != updated.DisplayName || listed[0].Slug != "research" {
		t.Fatal("list lost metadata")
	}
}
