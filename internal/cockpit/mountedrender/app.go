package mountedrender

import (
	"fmt"
	"reflect"
	"unsafe"

	tui "github.com/grindlemire/go-tui"
)

// NewApp returns a go-tui App backed by mock terminal/input objects.
// It is for Cockpit mounted snapshots and app-loop tests; it does not prove
// upstream raw-mode setup or the shipped PTY boundary.
func NewApp(width, height int) (*tui.App, *tui.MockEventReader, error) {
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
	if err := setAppField(app, "mounts", newMountState()); err != nil {
		return nil, nil, err
	}

	startUpdateBridge(app, stopCh, watcherQueue, updates, merged)

	return app, reader, nil
}

// String renders component as the root of a mounted go-tui app.
func String(component tui.Component, width, height int) (string, error) {
	app, _, err := NewApp(width, height)
	if err != nil {
		return "", err
	}
	defer app.Close()

	app.SetRootComponent(component)
	app.Render()
	return app.Buffer().String(), nil
}

// Trimmed renders component and strips trailing spaces from each line.
func Trimmed(component tui.Component, width, height int) (string, error) {
	app, _, err := NewApp(width, height)
	if err != nil {
		return "", err
	}
	defer app.Close()

	app.SetRootComponent(component)
	app.Render()
	return app.Buffer().StringTrimmed(), nil
}

func startUpdateBridge(
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

func newMountState() reflect.Value {
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
