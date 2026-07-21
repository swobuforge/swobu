package anthropic

import (
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/messages"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

// messagesCodec owns Anthropic's exact Messages request composition while the
// embedded standard codec retains shared response decoding.
type messagesCodec struct{ protocolcodec.Codec }

func (c messagesCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (messages.ProviderRequestDocument, error) {
		return messages.LowerProviderRequestDocument(req.Canonical, req.Delivery, sink, req.ExchangeID, messages.EncodeOptions{Compatibility: req.Compatibility})
	})
	if err != nil {
		return carrier.Document{}, decisions, protocolcodec.MarkUnsupportedByBackend(err)
	}
	for index := range document.Tools {
		if document.Tools[index].Type == canonical.ToolTypeWebSearch {
			document.Tools[index].Type = currentDirectWebSearchToolVersion
			document.Tools[index].AllowedCallers = []string{"direct"}
		}
	}
	encoded, err := messages.EncodeProviderRequestDocument(document)
	return encoded, decisions, err
}

var _ provider.Codec = messagesCodec{}
