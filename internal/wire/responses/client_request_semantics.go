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

func resolveClientRequestSemantics(dto responsesRequestDTO, marker bool) (clientRequestSemantics, error) {
	if !marker {
		return clientRequestSemantics{dialect: sourceDialectStandard}, nil
	}
	var items []responsesInputItemDTO
	if err := json.Unmarshal(dto.Input, &items); err != nil || len(items) == 0 ||
		strings.TrimSpace(items[0].Type) != "additional_tools" || strings.TrimSpace(items[0].Role) != "developer" {
		return clientRequestSemantics{}, canonical.BadRequest("Codex Responses Lite marker requires an additional_tools developer prefix")
	}
	prefixEnd := 1
	if len(items) > 1 && strings.TrimSpace(items[1].Type) == "message" && strings.TrimSpace(items[1].Role) == "developer" {
		prefixEnd = 2
	}
	return clientRequestSemantics{dialect: sourceDialectCodexLite, requestInputPrefixEnd: prefixEnd}, nil
}

func (s clientRequestSemantics) isLite() bool { return s.dialect == sourceDialectCodexLite }
