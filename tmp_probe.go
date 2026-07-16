package main

import (
    "context"
    "fmt"
    "reflect"
    "unsafe"

    tui "github.com/grindlemire/go-tui"
    workspace "github.com/swobuforge/swobu/internal/cockpit/pages/workspace"
    "github.com/swobuforge/swobu/internal/cockpit/readmodel"
    "github.com/swobuforge/swobu/internal/cockpit/testkit"
    ui "github.com/swobuforge/swobu/internal/cockpit/ui"
)

func main() {
    view := workspace.Page(readmodel.WorkspaceReadModel{
        Slug:        "dev",
        State:       readmodel.WorkspaceExisting,
        ClientBaseURL: "http://127.0.0.1:7926/c/dev",
        Routes: []readmodel.RouteReadModel{{
            ID:        "gpt",
            ModelName: "gpt",
            State:     readmodel.RouteNormal,
            Enabled:   true,
        }},
    }, nil, nil, nil, context.Background(), nil)
    view.RoutesSection.RequestAddRouteFocus()
    h, err := testkit.NewHarness(view)
    if err != nil {
        panic(err)
    }
    defer h.Close()
    h.Open()
    fmt.Println(h.Frame())
    app := h.App()
    mounts := reflect.ValueOf(app).Elem().FieldByName("mounts")
    mounts = reflect.NewAt(mounts.Type(), unsafe.Pointer(mounts.UnsafeAddr())).Elem()
    cache := mounts.Elem().FieldByName("cache")
    for _, key := range cache.MapKeys() {
        val := cache.MapIndex(key)
        if !val.IsValid() || val.IsNil() {
            continue
        }
        comp := val.Interface()
        if row, ok := comp.(*ui.SelectableRow); ok {
            fmt.Printf("row label=%q autofocus=%v focusedState=%v\n", row.Label, row.AutoFocus, row.FocusedState().Get())
        }
        if ep, ok := comp.(*workspace.PageView); ok {
            _ = ep
        }
    }

    root := h.App().Root()
    fmt.Printf("root type=%T\n", root)
    _ = root
    _ = tui.NewRef()
}
