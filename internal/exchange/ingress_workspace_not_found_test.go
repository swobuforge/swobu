package exchange

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/routing"
)

type missingWorkspaceLookup struct {
	calls int
	got   routing.WorkspaceSlug
}

func (l *missingWorkspaceLookup) GetWorkspace(_ context.Context, slug routing.WorkspaceSlug) (routing.Workspace, error) {
	l.calls++
	l.got = slug
	return routing.Workspace{}, routing.ErrNotFound
}

func TestIngressMissingWorkspaceUsesSlugSpecificBadEndpointForRequestsAndModels(t *testing.T) {
	for _, slugText := range []string{"default", "typo-workspace"} {
		t.Run(slugText, func(t *testing.T) {
			slug, err := routing.ParseWorkspaceSlug(slugText)
			if err != nil {
				t.Fatal(err)
			}
			lookup := &missingWorkspaceLookup{}
			ingress := NewIngress(lookup, nil, RuntimePoliciesSpec{})

			_, requestErr := ingress.HandleRequest(context.Background(), RequestInput{Workspace: slug})
			assertMissingWorkspaceError(t, requestErr, slugText)
			models, modelsErr := ingress.ListModels(context.Background(), ListModelsInput{Workspace: slug})
			assertMissingWorkspaceError(t, modelsErr, slugText)
			if models.DefaultModelID != "" || len(models.Models) != 0 {
				t.Fatalf("missing workspace synthesized models: %#v", models)
			}
			if lookup.calls != 2 || lookup.got != slug {
				t.Fatalf("lookup calls=%d slug=%q, want two calls for %q", lookup.calls, lookup.got, slug)
			}
		})
	}
}

func assertMissingWorkspaceError(t *testing.T, err error, slug string) {
	t.Helper()
	var swobuErr canonical.Error
	if !errors.As(err, &swobuErr) {
		t.Fatalf("error = %v, want canonical Swobu error", err)
	}
	if swobuErr.Code != canonical.ErrorCodeBadEndpoint || swobuErr.Origin != canonical.ErrorOriginSwobu {
		t.Fatalf("error = %#v, want Swobu BAD_ENDPOINT", swobuErr)
	}
	for _, want := range []string{slug, "does not exist", "Create it in Swobu", "check the workspace name"} {
		if !strings.Contains(swobuErr.Message, want) {
			t.Fatalf("message %q missing %q", swobuErr.Message, want)
		}
	}
}
