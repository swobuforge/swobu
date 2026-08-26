package target_config

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestAmbientOrReferenceTransitionsEmitOnlyCredentialReferences(t *testing.T) {
	var applied []string
	a := AmbientOrReferenceAuthentication(AmbientOrReferenceAuthenticationProps{
		ID: "auth", AmbientLabel: "Google identity (ADC)", ReferenceLabel: "Gemini API key",
		SuggestedEnvVar: "GEMINI_API_KEY", Ref: "env:GEMINI_API_KEY",
		Apply: func(ref string) { applied = append(applied, ref) },
	}).(*ambientOrReferenceAuthentication)

	a.useAmbient()
	a.chooseReference()
	a.chooser.Get().selectRef("file:/run/secrets/gemini")
	if got := strings.Join(applied, "|"); got != "|file:/run/secrets/gemini" {
		t.Fatalf("applied references = %q, want ambient clear then explicit file reference", got)
	}
	if got := a.ref.Get(); got != "file:/run/secrets/gemini" {
		t.Fatalf("display reference = %q", got)
	}
}

func TestAmbientOrReferenceDraftMutationInvalidatesCatalogEvidence(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.SelectProvider(string(profile.ProviderSpecGemini))
	w.Catalog.Set(catalogOperationState{Err: "stale authentication failure"})
	credential, ok := ambientOrReferenceCredential(w)
	if !ok {
		t.Fatal("Gemini profile did not select ambient/reference authoring")
	}
	props := ambientOrReferenceAuthenticationProps(w, credential)
	props.Apply("env:GEMINI_API_KEY")
	if got := w.Draft.Get().CredentialRef; got != "env:GEMINI_API_KEY" {
		t.Fatalf("draft credential = %q", got)
	}
	if catalog := w.Catalog.Get(); catalog.Err != "" || !catalog.Loading {
		t.Fatalf("catalog evidence survived authentication change: %#v", catalog)
	}
}

func TestMountedAmbientOrReferenceEscapeRetreatsOneLevelAtATime(t *testing.T) {
	a := ambientOrReferenceVisual("")
	h, err := testkit.NewHarnessAt(a, 100, 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.App().BlurFocused()
	h.FocusNext()
	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if frame := h.FrameTrimmed(); !strings.Contains(frame, "variable") {
		t.Fatalf("environment editor not mounted:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if frame := h.FrameTrimmed(); !strings.Contains(frame, "environment variable") || strings.Contains(frame, "variable            _") {
		t.Fatalf("first Escape did not return to credential chooser:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if frame := h.FrameTrimmed(); !strings.Contains(frame, "use Gemini API key") || strings.Contains(frame, "environment variable") {
		t.Fatalf("second Escape did not return to authentication menu:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if frame := h.FrameTrimmed(); strings.Contains(frame, "use Gemini API key") || !strings.Contains(frame, "Google identity (ADC)") {
		t.Fatalf("third Escape did not close authentication menu:\n%s", frame)
	}
}

func TestMountedAmbientOrReferenceHeaderEnterTogglesClosed(t *testing.T) {
	a := ambientOrReferenceVisual("")
	h, err := testkit.NewHarnessAt(a, 100, 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if frame := h.FrameTrimmed(); !strings.Contains(frame, "use Gemini API key") {
		t.Fatalf("menu not opened on first Enter:\n%s", frame)
	}
	// Focus is still on the header row ("close ↵"). Pressing Enter again toggles it closed.
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if frame := h.FrameTrimmed(); strings.Contains(frame, "use Gemini API key") {
		t.Fatalf("menu not closed when activating header row:\n%s", frame)
	}
}
