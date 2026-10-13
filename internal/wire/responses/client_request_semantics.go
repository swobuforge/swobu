package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// sourceDialect is resolved only at the Responses ingress boundary. It is not
// canonical conversation state and must disappear after request decoding.
type sourceDialect uint8

const (
	sourceDialectStandard sourceDialect = iota
	sourceDialectCodexLite
)

type clientRequestSemantics struct {
	dialect               sourceDialect
	requestInputPrefixEnd int
}

// The marked Lite prefix contains optional tools and optional base instructions.
// Codex rebuilds these request-owned items even for tool-less compaction prompts.
func resolveClientRequestSemantics(dto responsesRequestDTO, marker bool) (clientRequestSemantics, error) {
	if !marker {
		return clientRequestSemantics{dialect: sourceDialectStandard}, nil
	}
	if len(dto.Instructions) != 0 {
		var instructions string
		if err := json.Unmarshal(dto.Instructions, &instructions); err != nil || instructions != "" || strings.TrimSpace(string(dto.Instructions)) == "null" {
			return clientRequestSemantics{}, canonical.BadRequest("Responses Lite top-level instructions must be empty or absent")
		}
	}
	semantics := clientRequestSemantics{dialect: sourceDialectCodexLite}
	var items []responsesInputItemDTO
	if err := json.Unmarshal(dto.Input, &items); err != nil {
		// The ordinary input decoder validates strings and malformed input. Neither
		// carries a request-owned prefix, but the marker still selects Lite semantics.
		return semantics, nil
	}
	if len(items) > 0 && strings.TrimSpace(items[0].Type) == "additional_tools" && strings.TrimSpace(items[0].Role) == "developer" {
		semantics.requestInputPrefixEnd = 1
	}
	index := semantics.requestInputPrefixEnd
	if index < len(items) && strings.TrimSpace(items[index].Type) == "message" && strings.TrimSpace(items[index].Role) == "developer" {
		semantics.requestInputPrefixEnd = index + 1
	}
	return semantics, nil
}

func (s clientRequestSemantics) isLite() bool { return s.dialect == sourceDialectCodexLite }
