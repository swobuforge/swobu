package cli

import (
	"testing"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func TestRunnerAddrUsesSingleStartupResolution(t *testing.T) {
	runner := Runner{Addr: "127.0.0.1:65432"}
	resolved, err := platformconfig.ResolveStartupConfig(runner.Addr)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Addr != runner.Addr || platformconfig.BaseURL(resolved.Addr) != "http://127.0.0.1:65432" {
		t.Fatalf("resolved = %#v", resolved)
	}
}
