package authplane

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestStartAttemptCrashHelper(t *testing.T) {
	directory := os.Getenv("SWOBU_AUTH_START_CRASH_DIRECTORY")
	if directory == "" {
		return
	}
	attempts, err := OpenStartAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := NewAuthSessionManager(driverStub{startFn: func(context.Context, StartInput) (DriverStartResult, error) {
		fmt.Fprintln(os.Stdout, "AUTH_START_DISPATCH_READY")
		_, _ = os.Stdin.Read(make([]byte, 1))
		return DriverStartResult{SessionID: "unacknowledged"}, nil
	}})
	_, err = manager.StartAttempt(t.Context(), attempts, "process-death-start-key", StartInput{ProviderSpec: "chatgpt", DraftSubject: "draft", AuthMode: "device"}, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
}

func TestStartAttemptProcessDeathForbidsRedispatch(t *testing.T) {
	_, directory := privateStartAttempts(t)
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestStartAttemptCrashHelper$", "-test.count=1")
	command.Env = append(os.Environ(), "SWOBU_AUTH_START_CRASH_DIRECTORY="+directory)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "AUTH_START_DISPATCH_READY" {
				ready <- true
				return
			}
		}
		if err := scanner.Err(); err != nil {
			t.Errorf("dispatch boundary output: %v", err)
		}
		ready <- false
	}()
	select {
	case observed := <-ready:
		if !observed {
			t.Fatal("child exited before dispatch boundary")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dispatch boundary timed out")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("child was not killed")
	}
	attempts, err := OpenStartAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	manager, _ := NewAuthSessionManager(driverStub{startFn: func(context.Context, StartInput) (DriverStartResult, error) {
		starts++
		return DriverStartResult{SessionID: "replacement"}, nil
	}})
	_, err = manager.StartAttempt(t.Context(), attempts, "process-death-start-key", StartInput{ProviderSpec: "chatgpt", DraftSubject: "draft", AuthMode: "device"}, func() error { return nil })
	if !errors.Is(err, ErrStartIndeterminate) || starts != 0 {
		t.Fatalf("killed handoff redispatched: starts=%d error=%v", starts, err)
	}
}
