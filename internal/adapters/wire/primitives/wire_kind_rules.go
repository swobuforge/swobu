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
	WireOpEncode WireOperation = "encode"
	WireOpDecode WireOperation = "decode"
	WireOpPatch  WireOperation = "patch"
)

func (k WireKind) Supports(op WireOperation) bool {
	switch k {
	case WireKindRequest:
		return op == WireOpEncode || op == WireOpPatch
	case WireKindResponse, WireKindUsage, WireKindError:
		return op == WireOpDecode || op == WireOpPatch
	case WireKindResponseStream:
		return op == WireOpDecode || op == WireOpPatch
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

func (p WirePacket) ValidateFor(op WireOperation) error {
	if err := p.Kind.Validate(); err != nil {
		return err
	}
	if !p.Kind.Supports(op) {
		return fmt.Errorf("wire kind %q does not support operation %q", string(p.Kind), string(op))
	}
	return nil
}
