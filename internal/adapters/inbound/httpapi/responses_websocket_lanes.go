package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
)

// These bounds apply to one client-owned Responses WebSocket connection. They
// are not Node/Workspace capacity policy; each dispatched operation still
// passes the ordinary Workspace admission and limit checks. Active/name limits
// match the Responses WebSocket protocol. The 64-create queue is the accepted
// named-lanes product bound, protecting retained request bodies per socket;
// queue overflow rejects only that create. Each body is separately size-limited.
const (
	websocketRequestTypeResponseCreate = "response.create"
	maxResponsesWebsocketActive        = 16
	maxResponsesWebsocketNamedLanes    = 32
	maxResponsesWebsocketQueued        = 64
)

var (
	errWebsocketEventUnsupported  = errors.New("unsupported Responses WebSocket event type")
	errWebsocketWarmupUnsupported = errors.New("WebSocket generate:false warmup is not supported")
)

type websocketCreate struct {
	lane string // empty means the protocol's implicit default lane
	body []byte // response.create envelope without WebSocket-only keys
}

// parseWebsocketCreate validates transport metadata before enqueueing and
// removes it from the upstream Responses create payload. Preserve raw JSON
// values, including numeric lexemes, rather than decoding into float64.
func parseWebsocketCreate(raw []byte) (websocketCreate, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope == nil {
		return websocketCreate{}, errors.New("websocket request body must be a JSON object")
	}
	create := websocketCreate{}
	if lane, present := envelope["stream_id"]; present {
		if err := json.Unmarshal(lane, &create.lane); err != nil || !validWebsocketStreamID(create.lane) {
			return websocketCreate{}, errors.New("stream_id must be 1-256 letters, digits, underscores, hyphens or periods")
		}
	}
	var eventType string
	if err := json.Unmarshal(envelope["type"], &eventType); err != nil || eventType != websocketRequestTypeResponseCreate {
		return create, errWebsocketEventUnsupported
	}
	// Warmup is not model inference. Never turn a generate:false request into
	// a charged provider execution just because Swobu has no local warmup cache.
	if rawGenerate, present := envelope["generate"]; present {
		var generate bool
		if err := json.Unmarshal(rawGenerate, &generate); err != nil || string(rawGenerate) == "null" {
			return create, errors.New("generate must be a boolean")
		}
		if !generate {
			return create, errWebsocketWarmupUnsupported
		}
	}
	delete(envelope, "type")
	delete(envelope, "stream_id")
	delete(envelope, "generate")
	// WebSocket creates implicitly stream. The shared Responses decoder uses
	// the HTTP body flag to select delivery, so materialize that transport fact
	// here rather than requiring WebSocket clients to send an HTTP-only field.
	envelope["stream"] = json.RawMessage("true")
	body, err := json.Marshal(envelope)
	if err != nil {
		return create, errors.New("websocket create body is invalid")
	}
	create.body = body
	return create, nil
}

func validWebsocketStreamID(id string) bool {
	if len(id) == 0 || len(id) > 256 {
		return false
	}
	for i := 0; i < len(id); i++ {
		b := id[i]
		if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.') {
			return false
		}
	}
	return true
}

type websocketLane struct {
	queued []websocketCreate
	active bool
	ready  bool
}

// websocketLaneScheduler is owned by the single WebSocket read/dispatch loop.
// Its state is never accessed by workers, so no shared locking or persistent
// per-session state is necessary.
type websocketLaneScheduler struct {
	lanes  map[string]*websocketLane
	ready  []string
	active int
	queued int
	named  int
}

func (s *websocketLaneScheduler) enqueue(create websocketCreate) error {
	if s.queued >= maxResponsesWebsocketQueued {
		return errors.New("Responses WebSocket request queue is full")
	}
	if s.lanes == nil {
		s.lanes = make(map[string]*websocketLane)
	}
	lane, exists := s.lanes[create.lane]
	if !exists {
		if create.lane != "" {
			if s.named >= maxResponsesWebsocketNamedLanes {
				return fmt.Errorf("Responses WebSocket supports at most %d named stream IDs", maxResponsesWebsocketNamedLanes)
			}
			s.named++
		}
		lane = &websocketLane{}
		s.lanes[create.lane] = lane
	}
	lane.queued = append(lane.queued, create)
	s.queued++
	if !lane.active && !lane.ready {
		s.ready = append(s.ready, create.lane)
		lane.ready = true
	}
	return nil
}

func (s *websocketLaneScheduler) next() (websocketCreate, bool) {
	if s.active >= maxResponsesWebsocketActive {
		return websocketCreate{}, false
	}
	for len(s.ready) > 0 {
		id := s.ready[0]
		s.ready = s.ready[1:]
		lane := s.lanes[id]
		lane.ready = false
		if lane.active || len(lane.queued) == 0 {
			continue
		}
		request := lane.queued[0]
		lane.queued[0] = websocketCreate{}
		lane.queued = lane.queued[1:]
		lane.active = true
		s.active++
		s.queued--
		return request, true
	}
	return websocketCreate{}, false
}

func (s *websocketLaneScheduler) finish(id string) {
	lane := s.lanes[id]
	if lane == nil || !lane.active {
		panic("Responses WebSocket lane completion without active request")
	}
	lane.active = false
	s.active--
	if len(lane.queued) > 0 && !lane.ready {
		lane.ready = true
		s.ready = append(s.ready, id)
	}
}
