package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseWebsocketCreateKeepsTransportMetadataOutOfExchange(t *testing.T) {
	parsed, err := parseWebsocketCreate([]byte(`{"type":"response.create","stream_id":"agent.alpha-2","model":"test","input":"hi","number":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.lane != "agent.alpha-2" || strings.Contains(string(parsed.body), "stream_id") || strings.Contains(string(parsed.body), `"type"`) {
		t.Fatalf("malformed forwarded create: %#v", parsed)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(parsed.body, &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope["number"]) != "9007199254740993" || string(envelope["model"]) != `"test"` {
		t.Fatalf("create payload was altered: %s", parsed.body)
	}
	defaultLane, err := parseWebsocketCreate([]byte(`{"type":"response.create","model":"test"}`))
	if err != nil || defaultLane.lane != "" {
		t.Fatalf("default lane=%#v err=%v", defaultLane, err)
	}
}

func TestParseWebsocketCreateRejectsAmbiguousEventsAndBadStreamIDs(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `"text"`, `{`, `{}`, `{"type":123}`,
		`{"type":"response.create","stream_id":null}`,
		`{"type":"response.create","stream_id":""}`,
		`{"type":"response.create","stream_id":42}`,
		`{"type":"response.create","stream_id":"x/y"}`,
		`{"type":"response.create","stream_id":"é"}`,
		`{"type":"response.create","stream_id":"` + strings.Repeat("a", 257) + `"}`,
	} {
		if _, err := parseWebsocketCreate([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	parsed, err := parseWebsocketCreate([]byte(`{"type":"response.create","stream_id":"warmup","generate":false,"model":"test"}`))
	if !errors.Is(err, errWebsocketWarmupUnsupported) || parsed.lane != "warmup" {
		t.Fatalf("warmup was executed or lost lane: %#v %v", parsed, err)
	}
	parsed, err = parseWebsocketCreate([]byte(`{"type":"response.steer","stream_id":"valid_lane","input":"text"}`))
	if !errors.Is(err, errWebsocketEventUnsupported) || parsed.lane != "valid_lane" {
		t.Fatalf("unsupported event lost lane: %#v %v", parsed, err)
	}
}

func TestWebsocketSchedulerPreservesOrderWithinLaneAndAllowsOtherLanes(t *testing.T) {
	var queue websocketLaneScheduler
	for _, create := range []websocketCreate{
		{lane: "alpha", body: []byte(`{"input":"1"}`)},
		{lane: "alpha", body: []byte(`{"input":"2"}`)},
		{lane: "beta", body: []byte(`{"input":"3"}`)},
	} {
		if err := queue.enqueue(create); err != nil {
			t.Fatal(err)
		}
	}
	first, ok := queue.next()
	if !ok || first.lane != "alpha" || string(first.body) != `{"input":"1"}` {
		t.Fatalf("first %#v ok=%v", first, ok)
	}
	second, ok := queue.next()
	if !ok || second.lane != "beta" {
		t.Fatalf("independent lane did not start: %#v ok=%v", second, ok)
	}
	if _, ok := queue.next(); ok {
		t.Fatal("started two responses concurrently in one lane")
	}
	queue.finish("alpha")
	third, ok := queue.next()
	if !ok || third.lane != "alpha" || string(third.body) != `{"input":"2"}` {
		t.Fatalf("same lane FIFO failed: %#v ok=%v", third, ok)
	}
	queue.finish("beta")
	queue.finish("alpha")
	if queue.active != 0 || queue.queued != 0 {
		t.Fatalf("leaked scheduler state: %#v", queue)
	}
}

func TestWebsocketSchedulerCapsConcurrentLanesAndQueuesOverflow(t *testing.T) {
	var queue websocketLaneScheduler
	for i := range maxResponsesWebsocketActive + 4 {
		if err := queue.enqueue(websocketCreate{lane: fmt.Sprintf("lane_%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	for range maxResponsesWebsocketActive {
		if _, ok := queue.next(); !ok {
			t.Fatal("failed to start permitted concurrency")
		}
	}
	if _, ok := queue.next(); ok || queue.active != maxResponsesWebsocketActive || queue.queued != 4 {
		t.Fatal("exceeded in-flight limit")
	}
	queue.finish("lane_0")
	create, ok := queue.next()
	if !ok || create.lane != "lane_16" {
		t.Fatalf("queued different lane did not advance: %#v, %v", create, ok)
	}
}

func TestWebsocketSchedulerDistinctStreamAndQueueLimits(t *testing.T) {
	var queue websocketLaneScheduler
	for i := range maxResponsesWebsocketNamedLanes {
		if err := queue.enqueue(websocketCreate{lane: fmt.Sprintf("s%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.enqueue(websocketCreate{lane: "extra"}); err == nil {
		t.Fatal("accepted 33rd named lane")
	}
	if err := queue.enqueue(websocketCreate{lane: "s0"}); err != nil {
		t.Fatal("reused known lane was rejected", err)
	}
	if err := queue.enqueue(websocketCreate{lane: ""}); err != nil {
		t.Fatal("default lane counted against named lane limit", err)
	}
	for queue.queued < maxResponsesWebsocketQueued {
		if err := queue.enqueue(websocketCreate{lane: "s0"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.enqueue(websocketCreate{lane: "s0"}); err == nil {
		t.Fatal("unbounded pending memory")
	}
}

func TestParseWebsocketCreateMaterializesStreamingAndRemovesGenerate(t *testing.T) {
	for _, extra := range []string{"", `,"stream":false`, `,"generate":true`} {
		create, err := parseWebsocketCreate([]byte(`{"type":"response.create","model":"m"` + extra + `}`))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(create.body, &body); err != nil {
			t.Fatal(err)
		}
		if string(body["stream"]) != "true" || body["generate"] != nil {
			t.Fatalf("wrong transport projection: %s", create.body)
		}
	}
}
