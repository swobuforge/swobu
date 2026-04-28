package runtime_test

import (
	"context"
	"net"
	"strings"
	"testing"

	platformconfig "github.com/metrofun/swobu/internal/platform/config"
)

func TestRuntimeConfig_DefaultBindAddrIsLoopback(t *testing.T) {
	cfg := (platformconfig.RuntimeConfig{}).WithDefaults()
	if got := cfg.BindAddr; got != platformconfig.DefaultBindAddr() {
		t.Fatalf("bind addr = %q, want %q", got, platformconfig.DefaultBindAddr())
	}
}

func TestRuntimeBootstrap_BindsToLoopbackWhenStartedOnLoopback(t *testing.T) {
	daemon := startDaemon(t, runtimeFixture{})
	defer func() { _ = daemon.Close(context.Background()) }()

	if got := host(daemon.BindAddr()); got != "127.0.0.1" {
		t.Fatalf("host = %q, want %q", got, "127.0.0.1")
	}
	if !strings.HasPrefix(daemon.BaseURL(), "http://127.0.0.1:") {
		t.Fatalf("base url = %q, want loopback base URL", daemon.BaseURL())
	}
}

func host(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return host
}
