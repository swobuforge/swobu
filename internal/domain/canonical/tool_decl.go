package canonical

// ToolSchema stores semantic tool-input schema as one JSON object payload.
type ToolSchema struct {
	object *JSONObject
}

func EmptyToolSchema() ToolSchema {
	return ToolSchema{}
}

func NewToolSchemaObject(object JSONObject) ToolSchema {
	cloned := object.Clone()
	return ToolSchema{object: &cloned}
}

func (s ToolSchema) RawObject() string {
	if s.object == nil {
		return ""
	}
	return s.object.String()
}

func (s ToolSchema) IsEmpty() bool {
	return s.object == nil
}

func (s ToolSchema) Clone() ToolSchema {
	if s.object == nil {
		return EmptyToolSchema()
	}
	return NewToolSchemaObject(*s.object)
}

func cloneToolDeclarations(tools []ToolDeclaration) []ToolDeclaration {
	if tools == nil {
		return nil
	}
	cloned := make([]ToolDeclaration, len(tools))
	for i := range tools {
		cloned[i] = tools[i].Clone()
	}
	return cloned
}
