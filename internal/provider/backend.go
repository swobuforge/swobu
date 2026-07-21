package provider

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
)

// Codec owns final canonical/provider-wire conversion for one exact backend.
// Compatibility decisions are processing results; persisting them as evidence
// is outside this interface and cannot alter the codec result.
type Codec interface {
	Encode(Request) (carrier.Document, []compat.Decision, error)
	Decode(context.Context, Request, Ingress) (DecodedResponse, error)
}

// DecisionSource exposes compatibility decisions discovered only while a
// progressive provider response is consumed.
type DecisionSource interface {
	Decisions() []compat.Decision
}

// ResponsesOutputSource exposes a complete Responses-native continuation
// batch only after the provider response reaches a checkpoint-safe terminal.
// No layer outside Responses capture and checkpoint persistence interprets it.
type ResponsesOutputSource interface {
	ResponsesOutput() (responsesnative.Items, bool)
}

// DecodedResponse is one invocation-bound provider decode result. Portable
// semantics enter the canonical stream; independent Responses replay output
// remains beside it until checkpoint persistence.
type DecodedResponse struct {
	Stream            canonical.ResponseStream
	Decisions         []compat.Decision
	TerminalDecisions DecisionSource
	ResponsesOutput   ResponsesOutputSource
}

// Transport performs external I/O over a final provider wire document. It has
// no canonical request input and therefore cannot re-encode canonical state.
type Transport interface {
	Send(context.Context, carrier.Document) (Ingress, error)
}

// TransportFunc adapts one backend-bound send operation to Transport.
type TransportFunc func(context.Context, carrier.Document) (Ingress, error)

func (f TransportFunc) Send(ctx context.Context, document carrier.Document) (Ingress, error) {
	return f(ctx, document)
}

// BindTransport closes over the exact target during backend construction so a
// caller cannot substitute a second target after codec/key resolution.
func BindTransport(target TargetSnapshot, send func(context.Context, TargetSnapshot, carrier.Document) (Ingress, error)) Transport {
	bound := target.Clone()
	return TransportFunc(func(ctx context.Context, document carrier.Document) (Ingress, error) {
		ingress, err := send(ctx, bound, document)
		return ingress, NormalizeFailure(err)
	})
}

// BackendResolver composes one exact backend for a selected target snapshot.
// Resolution preserves the target exactly and performs no network I/O.
type BackendResolver interface {
	ResolveBackend(TargetSnapshot) (Backend, error)
}

// Backend binds one exact target to its native-resumption identity, codec, and
// document-only transport.
type Backend struct {
	Target    TargetSnapshot
	Codec     Codec
	Transport Transport
}

// Validate proves the backend is complete before exchange execution.
func (b Backend) Validate() error {
	if b.Target.ProviderSpec == "" || b.Target.TargetID == "" || b.Target.TargetVersion == 0 || b.Target.Model == "" {
		return errors.New("provider backend target is incomplete")
	}
	if err := b.Target.ValidateExecutionProtocol(); err != nil {
		return err
	}
	if b.Codec == nil {
		return errors.New("provider backend codec is nil")
	}
	if b.Transport == nil {
		return errors.New("provider backend transport is nil")
	}
	return nil
}
