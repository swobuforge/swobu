package opaqueturnstate

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/report"
)

const (
	// OpaqueThoughtSignature is one provider opaque turn-state field.
	OpaqueThoughtSignature = "thoughtSignature"
	thoughtStateStoreKey   = "provider.turnstate.thought_signature"
)

// Store persists opaque turn-state payloads between provider response and next request.
type Store interface {
	Put(key string, value string)
	Get(key string) (string, bool)
}

// Result reports capture/replay effects.
type Result struct {
	Mutated bool
	Losses  []report.Loss
}

// CaptureOpaqueFields extracts opaque provider fields from one provider response
// document and stores them for replay.
func CaptureOpaqueFields(doc carrier.WireDocument, store Store) Result {
	if store == nil || len(doc.Raw) == 0 {
		return Result{}
	}
	payload, err := carrier.DecodeWireDocumentJSON(doc)
	if err != nil {
		return Result{
			Losses: []report.Loss{{
				Field:    "output",
				Kind:     report.LossProviderPrivateStateMissing,
				Reason:   "opaque_capture_invalid_json",
				Severity: report.SeverityWarning,
			}},
		}
	}
	output, ok := payload["output"].([]any)
	if !ok {
		return Result{}
	}
	for _, entry := range output {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		content, ok := item["content"].([]any)
		if !ok {
			continue
		}
		for _, partEntry := range content {
			part, ok := partEntry.(map[string]any)
			if !ok {
				continue
			}
			value, _ := part[OpaqueThoughtSignature].(string)
			if strings.TrimSpace(value) == "" { // swobu:io-string source=provider-wire
				continue
			}
			store.Put(thoughtStateStoreKey, value)
			return Result{}
		}
	}
	return Result{}
}

// ReplayOpaqueFields injects required opaque state into one provider request
// carrier metadata when profile requires replay.
func ReplayOpaqueFields(doc carrier.WireDocument, store Store, requiresReplay bool) (carrier.WireDocument, Result, error) {
	if !requiresReplay || store == nil || len(doc.Raw) == 0 {
		return doc, Result{}, nil
	}
	state, ok := store.Get(thoughtStateStoreKey)
	if !ok || strings.TrimSpace(state) == "" { // swobu:io-string source=provider-wire
		return doc, Result{}, nil
	}
	next, changed, err := carrier.MutateWireDocumentJSON(doc, func(payload map[string]any) (bool, error) {
		current, ok := payload[OpaqueThoughtSignature].(string)
		if ok && current == state {
			return false, nil
		}
		payload[OpaqueThoughtSignature] = state
		return true, nil
	})
	if err != nil {
		return carrier.WireDocument{}, Result{}, fmt.Errorf("opaque replay payload is invalid json")
	}
	return next, Result{Mutated: changed}, nil
}
