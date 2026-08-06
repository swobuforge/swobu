package bedrock

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/provider"
)

// mantleResponsesCodec is the Bedrock Mantle isolation seam for Responses
// reasoning-replay identity (RFC G2 §7.6). It is the named site where any
// Bedrock-specific normalization of the Responses reasoning wire id would land.
//
// Today this is an identity decorator: encode and decode delegate unchanged to
// the provider-neutral Responses codec. That is deliberate, not dead code — the
// empirical probe (RFC §8.2/§8.4) showed xai.grok-4.3, the permanent
// closed-reasoning proxy vehicle, enforces no prefix rule on replay ids, so the
// proven normalization set is empty and "preserve only" (RFC §7.1–§7.5) ships
// without a rewrite. gpt-5.x (the model codex#28902 names) is permanently
// account-entitlement-blocked, so the §8.2 matrix cannot run against the real
// model and the prefix question stays open here. A proven normalization would
// later attach as one localized change to this type, mirroring
// mantleMessagesCodec.
type mantleResponsesCodec struct {
	provider.Codec
}

func (c mantleResponsesCodec) Encode(request provider.Request) (carrier.Document, []compat.Change, error) {
	return c.Codec.Encode(request)
}

func (c mantleResponsesCodec) Decode(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.Codec.Decode(ctx, request, ingress)
}
