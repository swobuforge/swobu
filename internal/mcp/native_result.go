package mcp

import (
	"encoding/json"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func validateNativeResult(result *sdk.CallToolResult) error {
	if result == nil {
		return fmt.Errorf("MCP source returned no result")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("MCP native result could not be encoded")
	}
	if len(encoded) > MaxToolResultBytes {
		return fmt.Errorf("MCP native result exceeds result limit")
	}
	return nil
}
