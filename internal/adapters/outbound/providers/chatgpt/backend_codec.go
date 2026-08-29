package chatgpt

import (
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func newBackendCodec(_ string) protocolcodec.Codec {
	falseVal := false
	return protocolcodec.Codec{
		Protocol: protocolkind.Responses,
		ResponsesDialect: protocolcodec.ResponsesDialect{
			ForceArrayInput:     true,
			OmitMaxOutputTokens: true,
			DefaultStore:        &falseVal,
			RequireStreamingSSE: true,
		},
	}
}
