// Package wire is the Swobu wire-protocol translation bounded context.
//
// It owns encode/decode between carrier-level wire formats and the canonical
// domain model. Sub-packages are organized by protocol family; shared helpers
// for OpenAI-shaped wire live under the openai/ sub-package.
//
// Dependency direction:
//
//	wire/* → compat (for descriptive changes)
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
	"github.com/swobuforge/swobu/internal/mcp"
)

// ClientCodec translates client-family wire documents into canonical requests
// and canonical outputs back into client-facing wire documents or byte streams.
type ClientCodec interface {
	DecodeClientRequest(doc carrier.Document) (ClientDecodeResult, error)
	EncodeResponseDocument(canonical.CanonicalRequest, canonical.CanonicalResponse) (ClientDocumentResult, error)
	EncodeResponseStream(context.Context, canonical.CanonicalRequest, canonical.ResponseStream, delivery.Delivery) (ClientByteStreamResult, error)
	EncodeResponseMessages(context.Context, canonical.CanonicalRequest, canonical.ResponseStream, delivery.Delivery) (ClientMessageResult, error)
}

type ClientDecodeResult struct {
	Request ClientRequestResult
	Changes []compat.Change
}

type ClientDocumentResult struct {
	Document carrier.Document
	Changes  []compat.Change
	// ResponseFingerprint is nil when the encoded response has no single
	// unambiguous client-history representation.
	ResponseFingerprint *historyfingerprint.Response
}

type ClientByteStreamResult struct {
	Stream  carrier.ByteStream
	Changes []compat.Change
	// Completion reports whether the logical terminal response was encoded and,
	// on success, carries its response fingerprint. Snapshot never blocks.
	Completion *ResponseCompletion
}

type ClientMessageResult struct {
	Response carrier.MessageResponse
	Changes  []compat.Change
	// Completion has the same lifecycle as ClientByteStreamResult.Completion.
	Completion *ResponseCompletion
}

// ClientRequestResult is the payload returned by DecodeClientRequest.
type ClientRequestResult struct {
	Request  canonical.CanonicalRequest
	Delivery delivery.Delivery
	// MCPAccess is request-private ingress state consumed by the local MCP
	// runtime or an exact native provider projection.
	MCPAccess mcp.Access
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
	Changes             []compat.Change
	Compatibility       compat.Summary
}

// ResponseCompletion is the codec-owned write-once completion cell.
type ResponseCompletion struct {
	mu                 sync.RWMutex
	snapshot           ResponseCompletionSnapshot
	baseChanges        []compat.Change
	progressiveChanges func() []compat.Change
	polyfilled         bool
}

// NewResponseCompletion returns a read-only observation plus codec-private
// completion functions. Result carriers expose only the observation.
func NewResponseCompletion() (*ResponseCompletion, func(*historyfingerprint.Response, []compat.Change), func(error)) {
	cell := &ResponseCompletion{}
	return cell, cell.complete, cell.fail
}

// ConfigureCompatibility installs reducer-owned winning-path facts before
// terminal consumption. Progressive changes are read only while completing.
func (c *ResponseCompletion) ConfigureCompatibility(base []compat.Change, progressive func() []compat.Change, polyfilled bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshot.State != CompletionPending {
		return
	}
	c.baseChanges = compat.CloneChanges(base)
	c.progressiveChanges = progressive
	c.polyfilled = polyfilled
}

// Complete records the only successful terminal response fingerprint.
func (c *ResponseCompletion) complete(fingerprint *historyfingerprint.Response, changes []compat.Change) {
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
	allChanges := compat.CloneChanges(c.baseChanges)
	if c.progressiveChanges != nil {
		allChanges = append(allChanges, c.progressiveChanges()...)
	}
	allChanges = append(allChanges, changes...)
	if err := compat.ValidateChanges(allChanges); err != nil {
		c.snapshot = ResponseCompletionSnapshot{State: CompletionFailed, Err: err}
		return
	}
	summary := compat.Summarize(allChanges, c.polyfilled)
	c.snapshot = ResponseCompletionSnapshot{
		State: CompletionCompleted, ResponseFingerprint: cloned,
		Changes: compat.CloneChanges(allChanges), Compatibility: summary,
	}
}

// Fail records terminal encoding failure without a usable fingerprint.
func (c *ResponseCompletion) fail(err error) {
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

func (c *ResponseCompletion) Snapshot() ResponseCompletionSnapshot {
	if c == nil {
		return ResponseCompletionSnapshot{State: CompletionFailed}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	snapshot := c.snapshot
	snapshot.Changes = compat.CloneChanges(snapshot.Changes)
	snapshot.Compatibility = snapshot.Compatibility.Clone()
	return snapshot
}

// ProviderEncodeInput is the declarative canonical input for provider encoders.
type ProviderEncodeInput struct {
	Request canonical.CanonicalRequest
	// MCPAccess is transient request state for native MCP projection.
	MCPAccess mcp.Access
}

// ProviderEncodeResult is the concrete provider-request lowering result.
type ProviderEncodeResult struct {
	Document carrier.Document
	Changes  []compat.Change
}

// ProviderDecodeResult makes all provider-owned decode outputs explicit.
// Progressive decision sources are returned deliberately by the codec that
// constructs the stream; durable fidelity lives in canonical events.
type ProviderDecodeResult struct {
	Stream             canonical.ResponseStream
	Changes            []compat.Change
	ProgressiveChanges func() []compat.Change
}
