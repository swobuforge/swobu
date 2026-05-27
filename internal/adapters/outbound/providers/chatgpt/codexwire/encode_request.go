package codexwire

import (
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

const codexDefaultInstructions = "You are a helpful assistant."

func EncodeRequest(request canonical.CanonicalRequest, _ bool) (core.WireRequest, error) {
	// Codex execute path is stream-native; batch clients are handled via
	// stream->batch projection outside this protocol encoder.
	store := false
	codec, err := protocolregistry.ForProtocolKind(protocolkind.Responses)
	if err != nil {
		return core.WireRequest{}, err
	}
	withOptions, ok := codec.(protocolregistry.ResponsesEncodeOptionsCodec)
	if !ok {
		return core.WireRequest{}, canonical.UnsupportedOperation("responses codec does not support options-based encode")
	}
	return withOptions.EncodeRequestWithOptions(request, true, protocolregistry.ResponsesEncodeOptions{
		Instructions:         codexDefaultInstructions,
		ForceStructuredInput: true,
		Store:                &store,
	})
}
