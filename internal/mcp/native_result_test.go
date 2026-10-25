package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNativeResultLimitIncludesEveryRetainedResultClass(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		result *sdk.CallToolResult
	}{
		{"missing", nil},
		{"text", &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("x", MaxToolResultBytes)}}}},
		{"image", &sdk.CallToolResult{Content: []sdk.Content{&sdk.ImageContent{Data: make([]byte, MaxToolResultBytes), MIMEType: "image/png"}}}},
		{"structured", &sdk.CallToolResult{StructuredContent: map[string]any{"large": strings.Repeat("x", MaxToolResultBytes)}}},
		{"metadata", &sdk.CallToolResult{Meta: sdk.Meta{"large": strings.Repeat("x", MaxToolResultBytes)}}},
		{"invalid", &sdk.CallToolResult{StructuredContent: make(chan int)}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateNativeResult(testCase.result); err == nil {
				t.Fatal("invalid or oversized native result admitted")
			}
		})
	}
}

func TestNativeResultLimitAcceptsExactEncodedBoundaryWithoutMutation(t *testing.T) {
	result := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: ""}}, IsError: true}
	empty, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*sdk.TextContent)
	text.Text = strings.Repeat("x", MaxToolResultBytes-len(empty))
	before, err := json.Marshal(result)
	if err != nil || len(before) != MaxToolResultBytes {
		t.Fatalf("boundary fixture bytes=%d err=%v", len(before), err)
	}
	if err := validateNativeResult(result); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(result)
	if err != nil || string(after) != string(before) {
		t.Fatal("native bound mutated result")
	}
	text.Text += "x"
	if err := validateNativeResult(result); err == nil {
		t.Fatal("one byte above native boundary admitted")
	}
}
