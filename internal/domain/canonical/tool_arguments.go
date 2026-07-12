package canonical

import "strings"

// ToolArguments stores semantic tool-call input as one raw payload string.
// Function tools usually validate this as a JSON object at the wire edge.
// Custom tools preserve the raw payload bytes without forcing object shape.
type ToolArguments struct {
	rawObject string
}

func EmptyToolArguments() ToolArguments {
	return ToolArguments{}
}

func NewToolArgumentsObject(raw string) ToolArguments {
	return ToolArguments{rawObject: raw}
}

func (a ToolArguments) RawObject() string {
	return a.rawObject
}

func (a ToolArguments) IsEmpty() bool {
	return strings.TrimSpace(a.rawObject) == "" // swobu:io-string source=domain
}
