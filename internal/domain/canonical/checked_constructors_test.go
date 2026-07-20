package canonical

func testToolSchema(raw string) ToolSchema {
	object, err := ParseJSONObject([]byte(raw))
	if err != nil {
		panic(err)
	}
	return NewToolSchemaObject(object)
}

func testRequestToolKey(kind ToolKind, name string) ToolKey {
	value, err := NewRequestToolKey(kind, name)
	if err != nil {
		panic(err)
	}
	return value
}
func testFunctionTool(key ToolKey, description string, schema ToolSchema, strict Specified[bool]) ToolDeclaration {
	value, err := NewFunctionTool(key, description, schema, strict)
	if err != nil {
		panic(err)
	}
	return value
}
func testCustomTool(key ToolKey, description string, format ToolFormat) ToolDeclaration {
	value, err := NewCustomTool(key, description, format)
	if err != nil {
		panic(err)
	}
	return value
}
func testMessageStart(author MessageRole) ItemStartPayload {
	v, e := NewMessageStart(author)
	if e != nil {
		panic(e)
	}
	return v
}
func testToolCallStart(id ToolCallID, key ToolKey) ItemStartPayload {
	v, e := NewToolCallStart(id, key)
	if e != nil {
		panic(e)
	}
	return v
}
