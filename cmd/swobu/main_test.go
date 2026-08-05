package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/inbound/cli"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func TestRootHelpDiscoversLauncherStartupControls(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommand(&cli.Runner{}, &stdout, &stderr, func() bool { return false })
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	help := stdout.String()
	for _, want := range []string{
		"--addr string",
		"daemon address for Cockpit attach-or-start (env: SWOBU_ADDR) (default: 127.0.0.1:7926)",
		"--config string",
		"daemon config path when Cockpit starts it (env: SWOBU_CONFIG_PATH)",
		"swobu --addr 127.0.0.1:9000 --config ./swobu.yaml",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q; help=%q", want, help)
		}
	}
}

func TestRootLauncherFlagsReachInteractiveRunner(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	t.Setenv(platformconfig.EnvDoNotTrack, "1")

	const addr = "127.0.0.1:9000"
	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	var attachAddr string
	var attachConfigPath string
	var cockpitAddr string
	runner := &cli.Runner{
		IsInteractive: func() bool { return true },
		AttachOrStart: func(_ context.Context, _ io.Writer, _ io.Writer, _ *http.Client, gotAddr, gotConfigPath string) error {
			attachAddr = gotAddr
			attachConfigPath = gotConfigPath
			return nil
		},
		LaunchInteractive: func(_ context.Context, _ io.Reader, _ io.Writer, _ io.Writer, gotAddr string) error {
			cockpitAddr = gotAddr
			return nil
		},
		Sleep: func(time.Duration) {},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommand(runner, &stdout, &stderr, func() bool { return true })
	root.SetArgs([]string{"--addr", addr, "--config", configPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v; stderr=%q", err, stderr.String())
	}
	if attachAddr != addr || cockpitAddr != addr {
		t.Fatalf("attach addr = %q, cockpit addr = %q, want %q", attachAddr, cockpitAddr, addr)
	}
	if attachConfigPath != configPath {
		t.Fatalf("attach config path = %q, want %q", attachConfigPath, configPath)
	}
}

func TestRootRejectsInvalidLauncherAddressBeforeEffects(t *testing.T) {
	var attached bool
	var launched bool
	runner := &cli.Runner{
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error {
			attached = true
			return nil
		},
		LaunchInteractive: func(context.Context, io.Reader, io.Writer, io.Writer, string) error {
			launched = true
			return nil
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommand(runner, &stdout, &stderr, func() bool { return true })
	root.SetArgs([]string{"--addr", "0.0.0.0:9000"})

	err := root.Execute()
	var exitErr *exitCodeError
	if !asExitCodeError(err, &exitErr) || exitErr.code != int(cli.ExitDown) {
		t.Fatalf("Execute error = %v, want exit code %d", err, cli.ExitDown)
	}
	if attached || launched {
		t.Fatalf("effects ran for invalid address: attached=%v launched=%v", attached, launched)
	}
	if !strings.Contains(stderr.String(), `address host "0.0.0.0" must be loopback`) {
		t.Fatalf("stderr missing validation error; stderr=%q", stderr.String())
	}
}
