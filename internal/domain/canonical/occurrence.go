package canonical

import "fmt"

// Occurrence identifies one repeated canonical semantic owner without mixing
// wire, route, or provider coordinates into capability identity. The zero value
// addresses a singular request- or response-wide capability.
type Occurrence struct {
	kind occurrenceKind
	item uint32
	part uint32
	tool ToolKey
	call ToolCallID
}

type occurrenceKind uint8

const (
	occurrenceRequestItem occurrenceKind = iota + 1
	occurrenceResponseItem
	occurrenceRequestPart
	occurrenceResponsePart
	occurrenceToolIndex
	occurrenceTool
	occurrenceCall
)

func RequestItemOccurrence(index uint32) Occurrence {
	return Occurrence{kind: occurrenceRequestItem, item: index}
}

func ResponseItemOccurrence(index uint32) Occurrence {
	return Occurrence{kind: occurrenceResponseItem, item: index}
}

func RequestPartOccurrence(ref RequestPartRef) Occurrence {
	return Occurrence{kind: occurrenceRequestPart, item: ref.Item, part: ref.Part}
}

func ResponsePartOccurrence(position ItemPosition) Occurrence {
	return Occurrence{kind: occurrenceResponsePart, item: position.Item, part: position.Part}
}

func ToolIndexOccurrence(index uint32) Occurrence {
	return Occurrence{kind: occurrenceToolIndex, item: index}
}

func ToolOccurrence(tool ToolKey) Occurrence {
	return Occurrence{kind: occurrenceTool, tool: tool.Clone()}
}

func CallOccurrence(call ToolCallID) Occurrence {
	return Occurrence{kind: occurrenceCall, call: call}
}

func (o Occurrence) IsZero() bool { return o.kind == 0 }

func (o Occurrence) RequestItem() (uint32, bool) {
	return o.item, o.kind == occurrenceRequestItem
}

func (o Occurrence) ResponseItem() (uint32, bool) {
	return o.item, o.kind == occurrenceResponseItem
}

func (o Occurrence) RequestPart() (RequestPartRef, bool) {
	return RequestPartRef{Item: o.item, Part: o.part}, o.kind == occurrenceRequestPart
}

func (o Occurrence) ResponsePart() (ItemPosition, bool) {
	return ItemPosition{Item: o.item, Part: o.part}, o.kind == occurrenceResponsePart
}

func (o Occurrence) Tool() (ToolKey, bool) {
	return o.tool.Clone(), o.kind == occurrenceTool
}

func (o Occurrence) ToolIndex() (uint32, bool) {
	return o.item, o.kind == occurrenceToolIndex
}

func (o Occurrence) Call() (ToolCallID, bool) {
	return o.call, o.kind == occurrenceCall
}

// Key returns one stable exchange-local comparison key for deduplicating
// repeated progressive observations. It is not a public or wire identity.
func (o Occurrence) Key() string {
	switch o.kind {
	case 0:
		return ""
	case occurrenceRequestItem:
		return fmt.Sprintf("request:%d", o.item)
	case occurrenceResponseItem:
		return fmt.Sprintf("response:%d", o.item)
	case occurrenceRequestPart:
		return fmt.Sprintf("request:%d:%d", o.item, o.part)
	case occurrenceResponsePart:
		return fmt.Sprintf("response:%d:%d", o.item, o.part)
	case occurrenceToolIndex:
		return fmt.Sprintf("tool-index:%d", o.item)
	case occurrenceTool:
		return "tool:" + o.tool.String()
	case occurrenceCall:
		return "call:" + o.call.String()
	default:
		return ""
	}
}
