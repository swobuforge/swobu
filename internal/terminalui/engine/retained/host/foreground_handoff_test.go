//go:build !race

package host

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

func TestRunForegroundClient_UnavailableWithoutRunner(t *testing.T) {
	_, err := RunForegroundClient(context.Background(), "claude", nil, nil)
	if !errors.Is(err, ErrForegroundHandoffUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestRunnerForegroundHandoff_ExecutesAndRejectsConcurrentLaunch(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init screen: %v", err)
	}
	defer screen.Fini()
	runner := New(screen, asView(bootRoot{}), struct{}{}, func(*struct{}, update.Action) []update.Effect {
		return nil
	})

	origCommand := runForegroundCommand
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	runForegroundCommand = func(context.Context, string, []string, map[string]string) (int, error) {
		started <- struct{}{}
		<-release
		return 0, nil
	}
	t.Cleanup(func() { runForegroundCommand = origCommand })

	unregister := registerForegroundRunner(runner.runForegroundHandoff)
	defer unregister()

	done := make(chan error, 1)
	go func() {
		_, err := RunForegroundClient(context.Background(), "claude", []string{"--help"}, map[string]string{"A": "B"})
		done <- err
	}()

	var call foregroundHandoffCall
	select {
	case call = <-runner.foregroundRequests:
	case <-time.After(time.Second):
		t.Fatal("foreground handoff was not queued")
	}
	go runner.executeForegroundHandoffCall(call)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("foreground command did not start")
	}

	_, err := RunForegroundClient(context.Background(), "codex", nil, nil)
	if !errors.Is(err, ErrForegroundHandoffActive) {
		t.Fatalf("err=%v", err)
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("foreground run failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("foreground run did not complete")
	}
}
