package exchange_test

import "github.com/swobuforge/swobu/internal/replay"

func testReplayScope() replay.Scope {
	return replay.Scope{Namespace: "alpha", CallerKey: "local"}
}
