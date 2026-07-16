package help

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestHelpPage_CopyDiagnosticsUpdatesVisibleStatus(t *testing.T) {
	page := View(readmodel.HelpReadModel{}, fakeHelpActions{
		copyDiagnostics: func(context.Context) (ports.DiagnosticsCopyResult, error) {
			return ports.DiagnosticsCopyResult{Status: ports.DiagnosticsCopyCopied, Text: "{}"}, nil
		},
	})

	page.CopyDiagnostics()

	if got := page.DiagnosticsStatus.Get(); got != readmodel.DiagnosticsCopied {
		t.Fatalf("diagnostics status = %v, want copied", got)
	}
	rendered := testkit.RenderMountedTrimmed(t, page, 90, 10)
	testkit.AssertVisual("diagnostics_copied").
		Fixture("testdata/help_surface/fixture/diagnostics_copied.txt").
		Viewport(90, 10).
		Now(t, rendered)
}

func TestHelpPage_CopyDiagnosticsFailureUpdatesVisibleStatus(t *testing.T) {
	page := View(readmodel.HelpReadModel{}, fakeHelpActions{
		copyDiagnostics: func(context.Context) (ports.DiagnosticsCopyResult, error) {
			return ports.DiagnosticsCopyResult{}, errors.New("clipboard unavailable")
		},
	})

	page.CopyDiagnostics()

	if got := page.DiagnosticsStatus.Get(); got != readmodel.DiagnosticsFailed {
		t.Fatalf("diagnostics status = %v, want failed", got)
	}
}

func TestHelpPage_UpdatePropsResetsCopiedStatusWhenHelpIdentityChanges(t *testing.T) {
	page := View(readmodel.HelpReadModel{
		Version:    "1.0.0",
		DocsURL:    "https://docs.example.com/v1",
		IssueURL:   "https://issues.example.com/v1",
		CommunityURL: "https://discord.example.com/v1",
	}, fakeHelpActions{})
	page.DiagnosticsStatus.Set(readmodel.DiagnosticsCopied)

	fresh := View(readmodel.HelpReadModel{
		Version:    "1.0.1",
		DocsURL:    "https://docs.example.com/v1",
		IssueURL:   "https://issues.example.com/v1",
		CommunityURL: "https://discord.example.com/v1",
	}, fakeHelpActions{})

	page.UpdateProps(fresh)

	if got := page.DiagnosticsStatus.Get(); got != readmodel.DiagnosticsReady {
		t.Fatalf("diagnostics status = %v, want ready after help identity change", got)
	}
}

func TestHelpPage_UpdatePropsPreservesCopiedStatusWhenHelpIdentityStaysSame(t *testing.T) {
	model := readmodel.HelpReadModel{
		Version:       "1.0.0",
		DocsURL:       "https://docs.example.com/v1",
		IssueURL:      "https://issues.example.com/v1",
		CommunityURL:  "https://discord.example.com/v1",
		CockpitVersion: "cockpit v2",
	}
	page := View(model, fakeHelpActions{})
	page.DiagnosticsStatus.Set(readmodel.DiagnosticsCopied)

	fresh := View(model, fakeHelpActions{})
	page.UpdateProps(fresh)

	if got := page.DiagnosticsStatus.Get(); got != readmodel.DiagnosticsCopied {
		t.Fatalf("diagnostics status = %v, want copied when help identity is unchanged", got)
	}
}

func TestHelpPage_ActionsUsePageContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var got context.Context
	page := ViewWithContext(readmodel.HelpReadModel{}, ctx, fakeHelpActions{
		copyDiagnostics: func(ctx context.Context) (ports.DiagnosticsCopyResult, error) {
			got = ctx
			return ports.DiagnosticsCopyResult{Status: ports.DiagnosticsCopyCopied, Text: "{}"}, nil
		},
	})

	page.CopyDiagnostics()

	if got == nil || got.Err() == nil {
		t.Fatalf("action context err = %v, want canceled context", got)
	}
}

func View(model readmodel.HelpReadModel, actions ports.HelpActions) *PageView {
	return ViewWithContext(model, context.Background(), actions)
}

type fakeHelpActions struct {
	copyDiagnostics func(context.Context) (ports.DiagnosticsCopyResult, error)
}

func (f fakeHelpActions) OpenDocs(context.Context) error {
	return nil
}

func (f fakeHelpActions) OpenCommunity(context.Context) error {
	return nil
}

func (f fakeHelpActions) OpenIssue(context.Context) error {
	return nil
}

func (f fakeHelpActions) CopyDiagnostics(ctx context.Context) (ports.DiagnosticsCopyResult, error) {
	if f.copyDiagnostics != nil {
		return f.copyDiagnostics(ctx)
	}
	return ports.DiagnosticsCopyResult{Status: ports.DiagnosticsCopyCopied, Text: "{}"}, nil
}
