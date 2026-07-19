package provider

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/carrier"
)

// Ingress is one truthful provider transport result variant.
type Ingress interface{ isProviderIngress() }

// DocumentIngress carries one buffered provider wire document.
type DocumentIngress struct{ Document carrier.Document }

func (DocumentIngress) isProviderIngress() {}

// StreamIngress carries one provider wire byte stream.
type StreamIngress struct{ Stream carrier.ByteStream }

func (StreamIngress) isProviderIngress() {}

// ValidateIngress proves one transport result is a non-empty provider-ingress
// carrier before the selected backend codec receives it.
func ValidateIngress(ingress Ingress) error {
	switch in := ingress.(type) {
	case DocumentIngress:
		if in.Document.IsEmpty() {
			return fmt.Errorf("provider ingress document is empty")
		}
		return nil
	case StreamIngress:
		if in.Stream.Body == nil {
			return fmt.Errorf("provider ingress stream is empty")
		}
		return nil
	case nil:
		return fmt.Errorf("provider ingress is nil")
	default:
		return fmt.Errorf("provider ingress %T is unsupported", ingress)
	}
}
