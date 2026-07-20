package provider

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// SupportsImagePlacement reports only what the selected protocol grammar can
// represent. Unknown protocols fail closed.
func SupportsImagePlacement(protocol protocolkind.ProtocolKind, placement canonical.ImagePlacement) bool {
	switch protocol {
	case protocolkind.ChatCompletions:
		return placement == canonical.ImageInMessage
	case protocolkind.Responses, protocolkind.Messages:
		return placement == canonical.ImageInMessage || placement == canonical.ImageInToolResult
	default:
		return false
	}
}
