package provider

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// Codec owns final canonical/provider-wire conversion for one exact backend.
// Compatibility changes are processing results; persisting them as evidence
// is outside this interface and cannot alter the codec result.
type Codec interface {
	Encode(Request) (carrier.Document, []compat.Change, error)
	Decode(context.Context, Request, Ingress) (DecodedResponse, error)
}

// DecodedResponse is one invocation-bound provider decode result. All durable
// response semantics enter the canonical stream.
type DecodedResponse struct {
	Stream canonical.ResponseStream
	// Changes contains facts known when decoding begins. ProgressiveChanges
	// returns the immutable facts accumulated once Stream reaches terminal.
	Changes            []compat.Change
	ProgressiveChanges func() []compat.Change
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
		if err == nil {
			return ingress, nil
		}
		if failure, ok := AsAttemptFailure(err); ok {
			return ingress, failure
		}
		return ingress, AttemptMayHaveExecuted(err)
	})
}

// BackendResolver composes one exact backend for a selected target snapshot.
// Resolution preserves the target exactly and performs no network I/O.
type BackendResolver interface {
	ResolveBackend(TargetSnapshot) (Backend, error)
}

// Backend binds one exact target generation to its codec and
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
