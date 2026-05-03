package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func defaultProtocolKindForProvider(providerSpec string) string {
	protocol, ok := providercatalog.DefaultProtocolForSpec(strings.TrimSpace(providerSpec))
	if !ok {
		return protocolsurface.ChatCompletions.String()
	}
	return protocol.String()
}
