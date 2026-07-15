package carrier

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// CarrierDocument is one protocol-family document carrier on a wire leg.
type CarrierDocument struct {
	Stage  Stage
	Family protocolkind.ProtocolKind
	Media  string
	Header http.Header
	Raw    []byte
	Meta   Meta
}

func NewCarrierDocument(
	stage Stage,
	family protocolkind.ProtocolKind,
	media string,
	header http.Header,
	raw []byte,
	meta Meta,
) CarrierDocument {
	clonedHeader := http.Header{}
	for k, values := range header {
		clonedHeader[k] = append([]string(nil), values...)
	}
	return CarrierDocument{
		Stage:  stage,
		Family: family,
		Media:  media,
		Header: clonedHeader,
		Raw:    append([]byte(nil), raw...),
		Meta:   meta.Clone(),
	}
}

func (d CarrierDocument) Clone() CarrierDocument {
	return NewCarrierDocument(d.Stage, d.Family, d.Media, d.Header, d.Raw, d.Meta)
}

func (d CarrierDocument) RawBytes() []byte {
	return append([]byte(nil), d.Raw...)
}

func (d CarrierDocument) IsEmpty() bool {
	return len(d.Raw) == 0
}
