package target_config

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

func TestFileCredentialBrowserSelectsOpaqueDaemonPathOnce(t *testing.T) {
	var applied []string
	row := newCredentialField(CredentialFieldProps{Apply: func(ref string) { applied = append(applied, ref) }})
	browser := FileCredentialBrowser(row)
	browser.OnSelect(`/home/operator/.config/provider.key`)
	if len(applied) != 1 || applied[0] != `file:/home/operator/.config/provider.key` {
		t.Fatalf("applied = %#v", applied)
	}
}

func TestFileCredentialBrowserPreservesWindowsDaemonPath(t *testing.T) {
	var applied string
	row := newCredentialField(CredentialFieldProps{Apply: func(ref string) { applied = ref }})
	FileCredentialBrowser(row).OnSelect(`C:\Users\operator\.config\provider.key`)
	if applied != `file:C:\Users\operator\.config\provider.key` {
		t.Fatalf("applied = %q", applied)
	}
}

func TestFileCredentialBrowserUsesExistingReferenceAsOpaqueInitialBrowsePath(t *testing.T) {
	const path = "/home/operator/.config/provider.key"
	row := newCredentialField(CredentialFieldProps{Ref: "file:" + path, BrowseFile: func(got string) (ui.FileBrowserListing, error) {
		if got != path {
			t.Fatalf("browse path = %q, want %q", got, path)
		}
		return ui.FileBrowserListing{Path: "/home/operator/.config"}, nil
	}})
	row.stage.Set(credStageMenu)
	row.openFile()
	if got := FileCredentialBrowser(row).CurrentDir.Get(); got != path {
		t.Fatalf("initial path = %q", got)
	}
}
