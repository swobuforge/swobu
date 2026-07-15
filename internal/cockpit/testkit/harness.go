package testkit

import (
	"fmt"
	"reflect"
	"unsafe"

	tui "github.com/grindlemire/go-tui"
)

// MockAppHarness is a lightweight, in-process interactive test fixture for go-tui
// components. It uses a mock terminal and a seeded App instance so cockpit
// tests stay deterministic without depending on a real PTY.
//
// This harness is intentionally unsafe: it reaches into go-tui App internals
// with reflect/unsafe so Swobu can prove its own usage against a seeded mock
// App. It does not prove the full upstream app loop.
//
// Use MockAppHarness for temporal tests that need focus management or event dispatch.
// For one-shot layout assertions, prefer RenderString / RenderBuffer.
type MockAppHarness struct {
	app    *tui.App
	reader *tui.MockEventReader
}

// NewHarness creates an interactive test fixture for the given root component.
func NewHarness(root tui.Component) (*MockAppHarness, error) {
	app, reader, err := newHarnessApp(120, 40)
	if err != nil {
		return nil, err
	}

	app.SetRootComponent(root)
	return &MockAppHarness{app: app, reader: reader}, nil
}

// NewFuncHarness creates a MockAppHarness from a bare element tree instead of a
// Component. It is useful when the production root (e.g., *Cockpit) renders via
// WorkspacePage surface components and you only need the outer element tree.
func NewFuncHarness(root *tui.Element) (*MockAppHarness, error) {
	app, reader, err := newHarnessApp(120, 40)
	if err != nil {
		return nil, err
	}

	app.SetRoot(root)
	return &MockAppHarness{app: app, reader: reader}, nil
}

// App returns the underlying go-tui App.
func (h *MockAppHarness) App() *tui.App { return h.app }

// Reader returns the MockEventReader so callers can inject events.
func (h *MockAppHarness) Reader() *tui.MockEventReader { return h.reader }

// Frame renders the component tree and returns the current buffer contents as
// a string. Focus must be established before rendering (FocusNext / traversal).
func (h *MockAppHarness) Frame() string {
	h.app.Render()
	return h.app.Buffer().String()
}

// DispatchKey sends a key event through the app's dispatch system and re-renders.
// Use this for deterministic keyboard tests with MockAppHarness.
func (h *MockAppHarness) DispatchKey(keyEvent tui.KeyEvent) {
	h.app.Dispatch(keyEvent)
	h.app.Render()
}

// FocusNext focuses the next focusable element and renders the frame.
func (h *MockAppHarness) FocusNext() {
	h.app.FocusNext()
}

// FocusPrev focuses the previous focusable element and renders the frame.
func (h *MockAppHarness) FocusPrev() {
	h.app.FocusPrev()
}

// Open renders the app once so dispatch tables and initial focus state are ready.
func (h *MockAppHarness) Open() {
	h.app.MarkDirty()
	h.app.Render()
}

// Close shuts down the harness.
// Do not use the harness after Close.
func (h *MockAppHarness) Close() {
	if h.app != nil {
		_ = h.app.Close()
	}
}

func newHarnessApp(width, height int) (*tui.App, *tui.MockEventReader, error) {
	app := &tui.App{}
	reader := tui.NewMockEventReader()
	terminal := tui.NewMockTerminal(width, height)
	buffer := tui.NewBuffer(width, height)
	stopCh := make(chan struct{})
	inputEvents := make(chan tui.Event, 256)
	updates := make(chan tui.Event, 256)
	merged := make(chan tui.Event, 256)
	watcherQueue := make(chan func(), 256)

	if err := setAppField(app, "terminal", reflect.ValueOf(terminal)); err != nil {
		return nil, nil, err
	}
	if err := setAppField(app, "reader", reflect.ValueOf(reader)); err != nil {
		return nil, nil, err
	}
	if err := setAppField(app, "buffer", reflect.ValueOf(buffer)); err != nil {
		return nil, nil, err
	}
	if err := setAppField(app, "stopCh", reflect.ValueOf(stopCh)); err != nil {
		return nil, nil, err
	}
	if err := setAppField(app, "inputEvents", reflect.ValueOf(inputEvents)); err != nil {
		return nil, nil, err
	}
	if err := setAppField(app, "updates", reflect.ValueOf(updates)); err != nil {
		return nil, nil, err
	}
	if err := setAppField(app, "merged", reflect.ValueOf(merged)); err != nil {
		return nil, nil, err
	}
	if err := setAppField(app, "watcherQueue", reflect.ValueOf(watcherQueue)); err != nil {
		return nil, nil, err
	}
	if err := setAppField(app, "mounts", newHarnessMountState()); err != nil {
		return nil, nil, err
	}

	startHarnessUpdateBridge(app, stopCh, watcherQueue, updates, merged)

	return app, reader, nil
}

func startHarnessUpdateBridge(
	app *tui.App,
	stopCh <-chan struct{},
	watcherQueue <-chan func(),
	updates <-chan tui.Event,
	merged chan<- tui.Event,
) {
	go func() {
		for {
			select {
			case fn := <-watcherQueue:
				if fn == nil {
					continue
				}
				app.QueueUpdate(fn)
			case <-stopCh:
				return
			}
		}
	}()

	go func() {
		for {
			select {
			case ev := <-updates:
				select {
				case merged <- ev:
				case <-stopCh:
					return
				}
			case <-stopCh:
				return
			}
		}
	}()
}

func newHarnessMountState() reflect.Value {
	appType := reflect.TypeOf(tui.App{})
	field, ok := appType.FieldByName("mounts")
	if !ok {
		panic("tui.App missing mounts field")
	}

	mounts := reflect.New(field.Type.Elem())
	mountsElem := mounts.Elem()
	for i := 0; i < mountsElem.NumField(); i++ {
		fv := mountsElem.Field(i)
		if fv.Kind() != reflect.Map {
			continue
		}
		reflect.NewAt(fv.Type(), unsafe.Pointer(fv.UnsafeAddr())).Elem().Set(reflect.MakeMap(fv.Type()))
	}

	return mounts
}

func setAppField(app *tui.App, name string, value reflect.Value) error {
	rv := reflect.ValueOf(app)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("tui.App must be a pointer to struct")
	}

	fv := rv.Elem().FieldByName(name)
	if !fv.IsValid() {
		return fmt.Errorf("tui.App missing field %q", name)
	}
	if !value.IsValid() {
		value = reflect.Zero(fv.Type())
	}
	if !value.Type().AssignableTo(fv.Type()) {
		return fmt.Errorf("field %q expects %s, got %s", name, fv.Type(), value.Type())
	}

	reflect.NewAt(fv.Type(), unsafe.Pointer(fv.UnsafeAddr())).Elem().Set(value)
	return nil
}
