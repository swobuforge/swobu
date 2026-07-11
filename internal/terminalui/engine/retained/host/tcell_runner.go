package host

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/loop"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type Runner[M any] struct {
	Screen   tcell.Screen
	Root     retained.ViewSpec[M]
	Loop     *loop.AppLoop[M]
	OnQuit   func()
	previous *paint.BufferPainter
	quit     bool

	foregroundInFlight atomic.Bool
	foregroundRequests chan foregroundHandoffCall
}

// New wires one retained root view to a real tcell screen.
func New[M any](screen tcell.Screen, root retained.ViewSpec[M], model M, reduce loop.Reducer[M]) *Runner[M] {
	runner := &Runner[M]{
		Screen:             screen,
		Root:               root,
		foregroundRequests: make(chan foregroundHandoffCall),
	}
	runner.Loop = loop.New(model, reduce)
	return runner
}

// Run owns the terminal event loop for the retained cockpit runtime.
func (r *Runner[M]) Run(ctx context.Context) error {
	if err := r.Screen.Init(); err != nil {
		return err
	}
	defer r.Screen.Fini()
	r.Screen.EnableMouse()
	unregisterForeground := registerForegroundRunner(r.runForegroundHandoff)
	defer unregisterForeground()

	events := make(chan tcell.Event, 16)
	quit := make(chan struct{})
	defer close(quit)
	go r.Screen.ChannelEvents(events, quit)

	r.Loop.SetContext(ctx)

	for {
		bounds := screenBounds(r.Screen)
		if r.Loop.NeedsRebuild() {
			if quit := r.settleAndFlush(ctx, bounds); quit {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			slog.Debug("tcell runner exiting on context cancellation", "error", ctx.Err())
			return ctx.Err()
		case call := <-r.foregroundRequests:
			r.executeForegroundHandoffCall(call)
		case ev := <-events:
			if ev == nil {
				continue
			}
			quit, resized := r.handleEvent(ev)
			if quit {
				slog.Debug("tcell runner exiting on quit event")
				return nil
			}
			if resized || r.Loop.NeedsRebuild() {
				bounds = screenBounds(r.Screen)
				if quit := r.settleAndFlush(ctx, bounds); quit {
					slog.Debug("tcell runner exiting on flush quit flag")
					return nil
				}
			}
		case actions := <-r.Loop.FollowUp():
			for _, a := range actions {
				r.Loop.Dispatch([]update.Action{a})
			}
			if r.Loop.NeedsRebuild() {
				bounds = screenBounds(r.Screen)
				if quit := r.settleAndFlush(ctx, bounds); quit {
					slog.Debug("tcell runner exiting on flush quit flag")
					return nil
				}
			}
		}
	}
}

func (r *Runner[M]) settleAndFlush(ctx context.Context, bounds geom.Rect) bool {
	for {
		if r.Loop.NeedsRebuild() {
			lifecycle := r.Loop.RebuildPending(r.Root, bounds)
			for _, eff := range lifecycle {
				r.runEffect(ctx, eff)
			}
			continue
		}
		select {
		case actions := <-r.Loop.FollowUp():
			for _, a := range actions {
				r.Loop.Dispatch([]update.Action{a})
			}
			continue
		default:
			return r.flush(bounds)
		}
	}
}

func (r *Runner[M]) runEffect(ctx context.Context, eff update.Effect) {
	go func() {
		actions := eff.Execute(ctx)
		r.Loop.Accept(actions)
	}()
}

func (r *Runner[M]) handleEvent(ev tcell.Event) (quit bool, resized bool) {
	switch current := ev.(type) {
	case *tcell.EventResize:
		r.Screen.Sync()
		r.Loop.Invalidate()
		return false, true
	case *tcell.EventKey:
		if current.Key() == tcell.KeyCtrlC {
			slog.Debug("tcell runner quit requested by Ctrl+C")
			r.quit = true
			if r.OnQuit != nil {
				r.OnQuit()
			}
			return true, false
		}
		mapped := mapKeyEvent(current)
		_ = r.Loop.DispatchEvent(mapped)
	case *tcell.EventMouse:
		_ = r.Loop.DispatchEvent(mapMouseEvent(current))
	}
	return false, false
}

func (r *Runner[M]) flush(bounds geom.Rect) bool {
	buffer := r.Loop.Render(bounds)
	flushBuffer(r.Screen, r.previous, buffer)
	r.previous = buffer
	r.Screen.Show()
	return r.quit
}

func flushBuffer(screen tcell.Screen, previous, next *paint.BufferPainter) {
	if screen == nil || next == nil {
		return
	}
	bounds := next.Bounds()
	for y := 0; y < bounds.H; y++ {
		for x := 0; x < bounds.W; x++ {
			cell := next.Cell(x, y)
			if previous != nil && previous.Cell(x, y) == cell {
				continue
			}
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			screen.SetContent(x, y, r, nil, tcell.StyleDefault)
		}
	}
}

func screenBounds(screen tcell.Screen) geom.Rect {
	width, height := screen.Size()
	return geom.Rect{W: width, H: height}
}

func mapKeyEvent(ev *tcell.EventKey) interaction.Event {
	key, r := mapTcellKey(ev.Key(), ev.Rune())
	mods := interaction.Modifiers(0)
	if ev.Modifiers()&tcell.ModShift != 0 {
		mods |= interaction.ModShift
	}
	if ev.Modifiers()&tcell.ModAlt != 0 {
		mods |= interaction.ModAlt
	}
	if ev.Modifiers()&tcell.ModCtrl != 0 {
		mods |= interaction.ModCtrl
	}
	return interaction.Event{Kind: interaction.EventKey, Key: key, Rune: r, Mods: mods}
}

func mapTcellKey(tk tcell.Key, r rune) (interaction.Key, rune) {
	switch tk {
	case tcell.KeyUp:
		return interaction.KeyUp, 0
	case tcell.KeyDown:
		return interaction.KeyDown, 0
	case tcell.KeyLeft:
		return interaction.KeyLeft, 0
	case tcell.KeyRight:
		return interaction.KeyRight, 0
	case tcell.KeyEsc:
		return interaction.KeyEsc, 0
	case tcell.KeyEnter:
		return interaction.KeyEnter, 0
	case tcell.KeyCtrlJ:
		return interaction.KeyEnter, 0
	case tcell.KeyTAB:
		return interaction.KeyTab, 0
	case tcell.KeyBacktab:
		return interaction.KeyShiftTab, 0
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return interaction.KeyBackspace, 0
	case tcell.KeyRune:
		if r == ' ' {
			return interaction.KeySpace, 0
		}
		if r == '\n' || r == '\r' {
			return interaction.KeyEnter, 0
		}
		if r == 0 {
			return interaction.KeyNone, 0
		}
		return interaction.KeyRune, r
	default:
		if r >= 0x20 && r != 0x7f {
			return interaction.KeyRune, r
		}
		return interaction.KeyNone, 0
	}
}

func mapMouseEvent(ev *tcell.EventMouse) interaction.Event {
	x, y := ev.Position()
	return interaction.Event{
		Kind: interaction.EventMouseDown,
		Pos:  geom.Point{X: x, Y: y},
	}
}
