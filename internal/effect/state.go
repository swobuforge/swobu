package effect

import "github.com/swobuforge/swobu/internal/turnstate"

type StateCaptureEffect struct {
	Key   turnstate.TurnStateKey
	Value []byte
}

func (StateCaptureEffect) Kind() Kind { return KindStateCapture }

type StateReplayEffect struct {
	Key turnstate.TurnStateKey
}

func (StateReplayEffect) Kind() Kind { return KindStateReplay }
