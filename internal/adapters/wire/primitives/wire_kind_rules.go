package core

import "fmt"

type WireKind string

const (
	WireKindUnknown        WireKind = ""
	WireKindRequest        WireKind = "request"
	WireKindResponse       WireKind = "response"
	WireKindResponseStream WireKind = "response_stream"
	WireKindUsage          WireKind = "usage"
	WireKindError          WireKind = "error"
)

type WireOperation string

const (
	WireOpEncode    WireOperation = "encode"
	WireOpDecode    WireOperation = "decode"
	WireOpTransform WireOperation = "transform"
)

func (k WireKind) Supports(op WireOperation) bool {
	switch k {
	case WireKindRequest:
		return op == WireOpEncode || op == WireOpTransform
	case WireKindResponse, WireKindUsage, WireKindError:
		return op == WireOpDecode || op == WireOpTransform
	case WireKindResponseStream:
		return op == WireOpDecode || op == WireOpTransform
	default:
		return false
	}
}

func (k WireKind) Validate() error {
	switch k {
	case WireKindRequest, WireKindResponse, WireKindResponseStream, WireKindUsage, WireKindError:
		return nil
	default:
		return fmt.Errorf("unsupported wire kind: %q", string(k))
	}
}

func ValidateWireDocumentFor(p WireDocument, op WireOperation) error {
	if err := p.Kind.Validate(); err != nil {
		return err
	}
	if !p.Kind.Supports(op) {
		return fmt.Errorf("wire kind %q does not support operation %q", string(p.Kind), string(op))
	}
	return nil
}
