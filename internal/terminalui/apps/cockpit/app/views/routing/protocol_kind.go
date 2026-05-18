package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func defaultProtocolKindForProvider(providerSpec string) string {
	if strings.EqualFold(strings.TrimSpace(providerSpec), "anthropic") { // swobu:io-string source=boundary
		return protocolkind.Messages.String()
	}
	return protocolkind.ChatCompletions.String()
}

func defaultSelectedFrameForProvider(providerSpec string) string {
	protocolKind := protocolkind.ProtocolKind(defaultProtocolKindForProvider(providerSpec))
	if frame, ok := providercatalog.DefaultFrameForSpecProtocol(providerSpec, protocolKind); ok {
		return frame
	}
	return providercatalog.FrameHTTPJSONBody
}
