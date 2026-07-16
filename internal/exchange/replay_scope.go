package exchange

import "github.com/swobuforge/swobu/internal/replay"

const unsafeLocalReplayCallerKey = "local"

// unsafeLocalReplayScope is the current single-user replay partition.
//
// TODO: replace this with authenticated caller identity before hosted/shared
// replay-addressed mode is exposed.
func unsafeLocalReplayScope(namespace string) replay.Scope {
	return replay.Scope{
		Namespace: namespace,
		CallerKey: unsafeLocalReplayCallerKey,
	}
}
