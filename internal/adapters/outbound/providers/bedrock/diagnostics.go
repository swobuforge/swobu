package bedrock

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

var bedrockValidationColumnPattern = regexp.MustCompile(`(?i)\bcolumn\s+([0-9]+)\b`)

type bedrockRequestShapeDiagnostic struct {
	RequestBytes    int
	InputShape      string
	InputItemCount  int
	InputTypeCounts string
	Column          int
	ColumnItemIndex int
	ColumnItemType  string
	ColumnItemRole  string
	ColumnContent   string
	ColumnItemBytes int
}

func logBedrockBackendDiagnostic(operation string, target provider.TargetSnapshot, requestPath string, backendErr canonical.BackendError) {
	msg := strings.TrimSpace(backendErr.Message) // swobu:io-string source=boundary
	if msg == "" {
		return
	}
	endpointClass, region := bedrockEndpointClassAndRegion(target.BaseURL)
	if requestPath != "" {
		operation = path.Clean(strings.TrimSpace(requestPath)) // swobu:io-string source=boundary
	}
	lower := strings.ToLower(msg) // swobu:io-string source=boundary
	switch {
	case strings.Contains(lower, "operation not allowed"):
		slog.Debug(
			"bedrock backend rejected operation",
			"operation", operation,
			"endpoint_class", endpointClass,
			"region", region,
			"status_code", backendErr.StatusCode,
			"target_id", backendErr.TargetID,
			"diagnostic", "model/operation is not invokable for current account+region+api path",
		)
	case strings.Contains(lower, "does not exist") && strings.Contains(lower, "model"):
		slog.Debug(
			"bedrock backend model missing on endpoint",
			"operation", operation,
			"endpoint_class", endpointClass,
			"region", region,
			"status_code", backendErr.StatusCode,
			"target_id", backendErr.TargetID,
			"diagnostic", "selected model id is unavailable on this endpoint/account/region",
		)
	}
}

func logBedrockRequestValidationDiagnostic(target provider.TargetSnapshot, requestPath string, backendErr canonical.BackendError, body []byte) {
	message := strings.TrimSpace(backendErr.Message)                    // swobu:io-string source=boundary
	if !strings.Contains(strings.ToLower(message), "invalid 'input'") { // swobu:io-string source=boundary
		return
	}
	diagnostic := inspectBedrockRequestShape(body, message)
	endpointClass, region := bedrockEndpointClassAndRegion(target.BaseURL)
	slog.Debug(
		"bedrock backend rejected request input shape",
		"operation", path.Clean(strings.TrimSpace(requestPath)), // swobu:io-string source=boundary
		"endpoint_class", endpointClass,
		"region", region,
		"status_code", backendErr.StatusCode,
		"target_id", backendErr.TargetID,
		"request_bytes", diagnostic.RequestBytes,
		"input_shape", diagnostic.InputShape,
		"input_item_count", diagnostic.InputItemCount,
		"input_type_counts", diagnostic.InputTypeCounts,
		"validation_column", diagnostic.Column,
		"column_item_index", diagnostic.ColumnItemIndex,
		"column_item_type", diagnostic.ColumnItemType,
		"column_item_role", diagnostic.ColumnItemRole,
		"column_content_types", diagnostic.ColumnContent,
		"column_item_bytes", diagnostic.ColumnItemBytes,
	)
}

func inspectBedrockRequestShape(body []byte, backendMessage string) bedrockRequestShapeDiagnostic {
	diagnostic := bedrockRequestShapeDiagnostic{RequestBytes: len(body), ColumnItemIndex: -1}
	match := bedrockValidationColumnPattern.FindStringSubmatch(backendMessage)
	if len(match) == 2 {
		diagnostic.Column, _ = strconv.Atoi(match[1])
	}
	var payload struct {
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(body, &payload) != nil {
		diagnostic.InputShape = "invalid"
		return diagnostic
	}
	input := bytes.TrimSpace(payload.Input)
	if len(input) == 0 || bytes.Equal(input, []byte("null")) {
		diagnostic.InputShape = "null"
		return diagnostic
	}
	if input[0] != '[' {
		diagnostic.InputShape = "scalar"
		return diagnostic
	}
	diagnostic.InputShape = "array"
	var items []json.RawMessage
	if json.Unmarshal(input, &items) != nil {
		return diagnostic
	}
	diagnostic.InputItemCount = len(items)
	typeCounts := make(map[string]int)
	inputOffset := bytes.Index(body, input)
	searchOffset := 0
	columnOffset := diagnostic.Column - 1 - inputOffset
	for index, item := range items {
		shape := inspectBedrockInputItemShape(item)
		typeCounts[shape.Type]++
		itemOffset := bytes.Index(input[searchOffset:], item)
		if itemOffset < 0 {
			continue
		}
		itemStart := searchOffset + itemOffset
		itemEnd := itemStart + len(item)
		if columnOffset >= itemStart && columnOffset <= itemEnd {
			diagnostic.ColumnItemIndex = index
			diagnostic.ColumnItemType = shape.Type
			diagnostic.ColumnItemRole = shape.Role
			diagnostic.ColumnContent = strings.Join(shape.ContentTypes, ",")
			diagnostic.ColumnItemBytes = len(item)
		}
		searchOffset = itemEnd
	}
	diagnostic.InputTypeCounts = formatBedrockInputTypeCounts(typeCounts)
	return diagnostic
}

type bedrockInputItemShape struct {
	Type         string
	Role         string
	ContentTypes []string
}

func inspectBedrockInputItemShape(raw json.RawMessage) bedrockInputItemShape {
	var item struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content any    `json:"content"`
		Output  any    `json:"output"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return bedrockInputItemShape{Type: "invalid"}
	}
	shape := bedrockInputItemShape{Type: strings.TrimSpace(item.Type), Role: strings.TrimSpace(item.Role)} // swobu:io-string source=boundary
	if shape.Type == "" {
		shape.Type = "missing"
	}
	shape.ContentTypes = bedrockValuePartTypes(item.Content)
	if len(shape.ContentTypes) == 0 {
		shape.ContentTypes = bedrockValuePartTypes(item.Output)
	}
	return shape
}

func bedrockValuePartTypes(value any) []string {
	parts, ok := value.([]any)
	if !ok {
		if value == nil {
			return nil
		}
		return []string{"scalar"}
	}
	types := make([]string, 0, len(parts))
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			types = append(types, "non_object")
			continue
		}
		partType, _ := part["type"].(string)
		partType = strings.TrimSpace(partType) // swobu:io-string source=boundary
		if partType == "" {
			partType = "missing"
		}
		types = append(types, partType)
	}
	return types
}

func formatBedrockInputTypeCounts(counts map[string]int) string {
	order := []string{"message", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "reasoning", "web_search_call", "tool_search_call", "tool_search_output"}
	parts := make([]string, 0, len(counts))
	for _, itemType := range order {
		if count := counts[itemType]; count > 0 {
			parts = append(parts, itemType+"="+strconv.Itoa(count))
			delete(counts, itemType)
		}
	}
	remaining := make([]string, 0, len(counts))
	for itemType := range counts {
		remaining = append(remaining, itemType)
	}
	sort.Strings(remaining)
	for _, itemType := range remaining {
		count := counts[itemType]
		parts = append(parts, itemType+"="+strconv.Itoa(count))
	}
	return strings.Join(parts, ",")
}

func bedrockEndpointClassAndRegion(baseURL string) (class string, region string) {
	class = "unknown"
	trimmed := strings.TrimSpace(baseURL) // swobu:io-string source=boundary
	if trimmed == "" {
		return class, ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return class, ""
	}
	host := strings.TrimSpace(strings.ToLower(u.Hostname())) // swobu:io-string source=boundary
	parts := strings.Split(host, ".")
	if len(parts) < 4 {
		return class, ""
	}
	switch {
	case strings.HasPrefix(parts[0], "bedrock-mantle") && parts[2] == "api" && parts[3] == "aws":
		class = "bedrock_mantle_openai_compat"
		region = strings.TrimSpace(parts[1]) // swobu:io-string source=boundary
	}
	return class, region
}
