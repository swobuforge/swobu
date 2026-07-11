package effect

import "github.com/swobuforge/swobu/internal/turnstate"

type StateCaptureEffect struct {
	Key   turnstate.Key
	Value []byte
}

func (StateCaptureEffect) Kind() Kind { return KindStateCapture }

type StateReplayEffect struct {
	Key turnstate.Key
}

func (StateReplayEffect) Kind() Kind { return KindStateReplay }
