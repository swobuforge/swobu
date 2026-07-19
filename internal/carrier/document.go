package carrier

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// Document is one protocol-family document carrier on a wire leg.
type Document struct {
	Family protocolkind.ProtocolKind
	Media  string
	Header http.Header
	Raw    []byte
	Meta   Meta
}

func NewDocument(
	family protocolkind.ProtocolKind,
	media string,
	header http.Header,
	raw []byte,
	meta Meta,
) Document {
	clonedHeader := http.Header{}
	for k, values := range header {
		clonedHeader[k] = append([]string(nil), values...)
	}
	return Document{
		Family: family,
		Media:  media,
		Header: clonedHeader,
		Raw:    append([]byte(nil), raw...),
		Meta:   meta.Clone(),
	}
}

func (d Document) Clone() Document {
	return NewDocument(d.Family, d.Media, d.Header, d.Raw, d.Meta)
}

func (d Document) RawBytes() []byte {
	return append([]byte(nil), d.Raw...)
}

func (d Document) IsEmpty() bool {
	return len(d.Raw) == 0
}
