package core

import "github.com/swobuforge/swobu/internal/carrier"

// CarrierWireDocumentFromWireDocument bridges one existing wire document to the
// new carrier contract.
func CarrierWireDocumentFromWireDocument(doc WireDocument, leg carrier.Leg, meta carrier.Meta) carrier.WireDocument {
	return carrier.WireDocument{
		Leg:    leg,
		Family: doc.Protocol,
		Media:  "application/json",
		Header: doc.Headers,
		Raw:    append([]byte(nil), doc.RawBody...),
		Meta:   meta,
	}
}

// WireDocumentFromCarrierWireDocument bridges one carrier wire document to the
// existing wire document contract.
func WireDocumentFromCarrierWireDocument(doc carrier.WireDocument) WireDocument {
	return WireDocument{
		Kind:     WireKindRequest,
		Protocol: doc.Family,
		Headers:  doc.Header,
		RawBody:  append([]byte(nil), doc.Raw...),
	}
}

// CarrierWireStreamFromWireStream bridges one existing stream carrier to
// the new carrier contract.
func CarrierWireStreamFromWireStream(stream WireStream, leg carrier.Leg, meta carrier.Meta) carrier.WireStream {
	return carrier.WireStream{
		Leg:     leg,
		Family:  stream.Protocol,
		Framing: carrier.Framing(stream.Framing),
		Header:  stream.Headers,
		Frames:  carrier.FrameReaderFromReadCloser(stream.Body),
		Meta:    meta,
	}
}
