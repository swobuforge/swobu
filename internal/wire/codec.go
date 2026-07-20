// Package wire is the Swobu wire-protocol translation bounded context.
//
// It owns encode/decode between carrier-level wire formats and the canonical
// domain model. Sub-packages are organized by protocol family; shared helpers
// for OpenAI-shaped wire live under the openai/ sub-package.
//
// Dependency direction:
//
//	wire/* → compat (for descriptive decisions)
//	wire/* → carrier (for Document, ByteStream)
//	wire/* → domain/canonical (for domain model)
//	wire/* → domain/historyfingerprint (for opaque codec-owned leaves)
//	exchange → wire (for codec interfaces)
//	wire does NOT import exchange.
package wire

import (
	"context"
	"sync"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/provider"
)

// ClientCodec translates client-family wire documents into canonical requests
// and canonical outputs back into client-facing wire documents or byte streams.
type ClientCodec interface {
	DecodeClientRequest(doc carrier.Document) (ClientDecodeResult, error)
	EncodeResponseDocument(canonical.CanonicalResponse) (ClientDocumentResult, error)
	EncodeResponseStream(context.Context, canonical.ResponseStream, delivery.Delivery) (ClientByteStreamResult, error)
	EncodeResponseMessages(context.Context, canonical.ResponseStream, delivery.Delivery) (ClientMessageResult, error)
}

type ClientDecodeResult struct {
	Request   ClientRequestResult
	Decisions []compat.Decision
}

type ClientDocumentResult struct {
	Document  carrier.Document
	Decisions []compat.Decision
	// ResponseFingerprint is nil when the encoded response has no single
	// unambiguous client-history representation.
	ResponseFingerprint *historyfingerprint.Response
}

type ClientByteStreamResult struct {
	Stream    carrier.ByteStream
	Decisions []compat.Decision
	// Completion reports whether the logical terminal response was encoded and,
	// on success, carries its response fingerprint. Snapshot never blocks.
	Completion ResponseCompletion
}

type ClientMessageResult struct {
	Response  carrier.MessageResponse
	Decisions []compat.Decision
	// Completion has the same lifecycle as ClientByteStreamResult.Completion.
	Completion ResponseCompletion
}

// ClientRequestResult is the payload returned by DecodeClientRequest.
type ClientRequestResult struct {
	Request  canonical.CanonicalRequest
	Delivery delivery.Delivery
	// RequestFingerprint identifies the current protocol-native contribution
	// relative to the predecessor selected by decode semantics.
	RequestFingerprint historyfingerprint.Request
	// RebasedRequest atomically carries the completed visible history and the
	// complete current invocation expressed relative to it. Nil means decode
	// reconstructed no implicit predecessor.
	RebasedRequest *RebasedRequest
}

// RebasedRequest couples one reconstructed completed history with the complete
// current invocation after only that historical prefix has been removed.
type RebasedRequest struct {
	Previous historyfingerprint.History
	Request  canonical.CanonicalRequest
}

// CompletionState is the write-once lifecycle of streamed response
// fingerprinting.
type CompletionState uint8

const (
	CompletionPending CompletionState = iota
	CompletionCompleted
	CompletionFailed
)

// ResponseCompletionSnapshot is one non-blocking observation of streamed
// client response encoding. Failed and pending snapshots carry no usable
// response fingerprint.
type ResponseCompletionSnapshot struct {
	State               CompletionState
	Err                 error
	ResponseFingerprint *historyfingerprint.Response
}

// ResponseCompletion exposes only observation of a codec-owned write-once
// completion cell.
type ResponseCompletion interface {
	Snapshot() ResponseCompletionSnapshot
}

type responseCompletion struct {
	mu       sync.RWMutex
	snapshot ResponseCompletionSnapshot
}

// NewResponseCompletion returns a read-only observation plus codec-private
// completion functions. Result carriers expose only the observation.
func NewResponseCompletion() (ResponseCompletion, func(*historyfingerprint.Response), func(error)) {
	cell := &responseCompletion{}
	return cell, cell.complete, cell.fail
}

// Complete records the only successful terminal response fingerprint.
func (c *responseCompletion) complete(fingerprint *historyfingerprint.Response) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshot.State != CompletionPending {
		return
	}
	var cloned *historyfingerprint.Response
	if fingerprint != nil {
		value := *fingerprint
		cloned = &value
	}
	c.snapshot = ResponseCompletionSnapshot{State: CompletionCompleted, ResponseFingerprint: cloned}
}

// Fail records terminal encoding failure without a usable fingerprint.
func (c *responseCompletion) fail(err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshot.State != CompletionPending {
		return
	}
	c.snapshot = ResponseCompletionSnapshot{State: CompletionFailed, Err: err}
}

func (c *responseCompletion) Snapshot() ResponseCompletionSnapshot {
	if c == nil {
		return ResponseCompletionSnapshot{State: CompletionFailed}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

// ProviderEncodeInput is the declarative canonical input for provider encoders.
type ProviderEncodeInput struct {
	Request canonical.CanonicalRequest
}

// ProviderEncodeResult is the concrete provider-request lowering result.
type ProviderEncodeResult struct {
	Document  carrier.Document
	Decisions []compat.Decision
}

// ProviderDecodeResult makes all provider-owned decode outputs explicit.
// Progressive decision sources are returned deliberately by the codec that
// constructs the stream; durable fidelity lives in canonical events.
type ProviderDecodeResult struct {
	Stream            canonical.ResponseStream
	Decisions         []compat.Decision
	TerminalDecisions provider.DecisionSource
}
