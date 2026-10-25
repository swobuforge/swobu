package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	segmentjson "github.com/segmentio/encoding/json"
)

func restoreNativeResponse(result sdk.Result, raw json.RawMessage) error {
	decode := func(value any) error {
		decoder := segmentjson.NewDecoder(bytes.NewReader(raw))
		decoder.DontMatchCaseInsensitiveStructFields()
		decoder.UseNumber()
		return decoder.Decode(value)
	}
	switch value := result.(type) {
	case *sdk.ListToolsResult:
		return decode(value)
	case *sdk.CallToolResult:
		var fields struct {
			Meta       sdk.Meta `json:"_meta"`
			Structured any      `json:"structuredContent"`
			Content    []struct {
				Meta     sdk.Meta `json:"_meta"`
				Resource *struct {
					Meta sdk.Meta `json:"_meta"`
				} `json:"resource"`
			} `json:"content"`
		}
		if err := decode(&fields); err != nil {
			return err
		}
		if len(fields.Content) != len(value.Content) {
			return fmt.Errorf("MCP native content decode mismatch")
		}
		value.Meta, value.StructuredContent = fields.Meta, fields.Structured
		for index, content := range value.Content {
			meta := fields.Content[index].Meta
			switch item := content.(type) {
			case *sdk.TextContent:
				item.Meta = meta
			case *sdk.ImageContent:
				item.Meta = meta
			case *sdk.AudioContent:
				item.Meta = meta
			case *sdk.ResourceLink:
				item.Meta = meta
			case *sdk.EmbeddedResource:
				item.Meta = meta
				if item.Resource != nil && fields.Content[index].Resource != nil {
					item.Resource.Meta = fields.Content[index].Resource.Meta
				}
			default:
				return fmt.Errorf("MCP native content cannot retain exact metadata")
			}
		}
	}
	return nil
}
