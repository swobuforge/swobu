package e2e_test

import (
	"testing"

	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestDaemonProcessUninitialized_ReportsReachableButEmpty(t *testing.T) {
	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{})

	status, exitCode, err := daemon.Status()
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if exitCode != 1 {
		t.Fatalf("status exit code = %d, want 1", exitCode)
	}
	if status.State != "uninitialized" {
		t.Fatalf("state = %q, want uninitialized", status.State)
	}
	if status.EndpointCount != 0 {
		t.Fatalf("endpoint_count = %d, want 0", status.EndpointCount)
	}
}
