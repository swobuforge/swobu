// Command webui-daemon starts an isolated real daemon for browser admission
// proof. It uses a private temporary configuration and never installs UI assets.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/swobuforge/swobu/internal/bootstrap"
	"github.com/swobuforge/swobu/internal/platform/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	directory, err := os.MkdirTemp("", "swobu-web-browser-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	daemon, err := bootstrap.Start(ctx, bootstrap.StartInput{ConfigPath: filepath.Join(directory, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:4179"}})
	if err != nil {
		return err
	}
	defer daemon.Close()
	fmt.Println("Web proof daemon ready: " + daemon.BaseURL())
	<-ctx.Done()
	return nil
}
