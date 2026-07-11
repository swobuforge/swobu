package canonical

import "strings"

// ToolArguments stores semantic tool-call input as one object payload
// payload string. The canonical core does not expose dynamic map payloads.
type ToolArguments struct {
	rawObject string
}

func EmptyToolArguments() ToolArguments {
	return ToolArguments{}
}

func NewToolArgumentsObject(raw string) ToolArguments {
	return ToolArguments{rawObject: strings.TrimSpace(raw)} // swobu:io-string source=domain
}

func (a ToolArguments) RawObject() string {
	return a.rawObject
}

func (a ToolArguments) IsEmpty() bool {
	return strings.TrimSpace(a.rawObject) == "" // swobu:io-string source=domain
}
