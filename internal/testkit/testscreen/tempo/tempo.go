package tempo

import (
	"fmt"
	"time"
)

// Eventually runs probe until it succeeds or timeout expires.
func Eventually(timeout, interval time.Duration, probe func() bool) error {
	if timeout <= 0 {
		return fmt.Errorf("timeout must be > 0")
	}
	if probe == nil {
		return fmt.Errorf("probe is required")
	}
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		if probe() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("condition not satisfied within %s", timeout)
		}
		time.Sleep(interval)
	}
}
