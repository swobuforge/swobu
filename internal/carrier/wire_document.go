package carrier

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// WireDocument is one protocol-family document carrier on a wire leg.
type WireDocument struct {
	Stage  Stage
	Family protocolkind.ProtocolKind
	Media  string
	Header http.Header
	Raw    []byte
	Meta   Meta
}

func NewWireDocument(
	stage Stage,
	family protocolkind.ProtocolKind,
	media string,
	header http.Header,
	raw []byte,
	meta Meta,
) WireDocument {
	clonedHeader := http.Header{}
	for k, values := range header {
		clonedHeader[k] = append([]string(nil), values...)
	}
	return WireDocument{
		Stage:  stage,
		Family: family,
		Media:  media,
		Header: clonedHeader,
		Raw:    append([]byte(nil), raw...),
		Meta:   meta.Clone(),
	}
}

func (d WireDocument) Clone() WireDocument {
	return NewWireDocument(d.Stage, d.Family, d.Media, d.Header, d.Raw, d.Meta)
}

func (d WireDocument) RawBytes() []byte {
	return append([]byte(nil), d.Raw...)
}

func (d WireDocument) IsEmpty() bool {
	return len(d.Raw) == 0
}
