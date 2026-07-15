package testkit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/grindlemire/go-tui"
)

func TestHarness_CanConstructFromComponent(t *testing.T) {
	// Use a simple struct component that satisfies tui.Component
	root := &simpleComp{el: tui.New(tui.WithText("hello"))}

	h, err := NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	got := h.Frame()
	if !strings.Contains(got, "hello") {
		t.Fatalf("frame missing 'hello', got:\n%s", got)
	}
}

type simpleComp struct {
	el *tui.Element
}

func (s *simpleComp) Render(app *tui.App) *tui.Element {
	return s.el
}

func TestHarness_CanConstructFromElement(t *testing.T) {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
	)
	root.AddChild(tui.New(tui.WithText("A")))
	root.AddChild(tui.New(tui.WithText("B")))

	h, err := NewFuncHarness(root)
	if err != nil {
		t.Fatalf("NewFuncHarness: %v", err)
	}
	defer h.Close()

	got := h.Frame()
	if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
		t.Fatalf("frame missing expected content: %q", got)
	}
}

func TestHarness_FocusNextRenders(t *testing.T) {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
	)
	root.AddChild(tui.New(
		tui.WithText("A"),
		tui.WithOnFocus(func(*tui.Element) {}),
	))
	root.AddChild(tui.New(
		tui.WithText("B"),
		tui.WithOnFocus(func(*tui.Element) {}),
	))

	h, err := NewFuncHarness(root)
	if err != nil {
		t.Fatalf("NewFuncHarness: %v", err)
	}
	defer h.Close()

	h.App().FocusNext()
	got := h.Frame()
	if !strings.Contains(got, "A") || !strings.Contains(got, "B") {
		t.Fatalf("frame missing expected content after focus: %q", got)
	}
}

func TestHarness_AppInternalsContract(t *testing.T) {
	appType := reflect.TypeOf(tui.App{})
	for _, fieldName := range []string{"terminal", "reader", "buffer", "stopCh", "inputEvents", "updates", "merged", "watcherQueue", "mounts"} {
		field, ok := appType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("MockAppHarness drift: tui.App missing private field %q", fieldName)
		}
		if field.PkgPath == "" {
			t.Fatalf("MockAppHarness drift: tui.App field %q is no longer private; revisit harness contract", fieldName)
		}
	}
}

func TestHarness_MountStateContract(t *testing.T) {
	mounts := newHarnessMountState()
	if mounts.Kind() != reflect.Ptr {
		t.Fatalf("newHarnessMountState kind = %s, want ptr", mounts.Kind())
	}

	mountsElem := mounts.Elem()
	if mountsElem.Kind() != reflect.Struct {
		t.Fatalf("newHarnessMountState elem kind = %s, want struct", mountsElem.Kind())
	}
	if mountsElem.NumField() == 0 {
		t.Fatal("MockAppHarness drift: tui.App mounts struct has no fields")
	}

	mapFields := 0
	for i := 0; i < mountsElem.NumField(); i++ {
		fv := mountsElem.Field(i)
		if fv.Kind() != reflect.Map {
			continue
		}
		mapFields++
		if fv.IsNil() {
			t.Fatalf("MockAppHarness drift: mounts map field %d is nil", i)
		}
	}
	if mapFields == 0 {
		t.Fatal("MockAppHarness drift: tui.App mounts struct no longer contains map fields")
	}
}
