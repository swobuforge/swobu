package delivery

type Conversion uint8

const (
	Passthrough Conversion = iota
	CollectStreamToDocument
	SynthesizeDocumentToStream
)

func (c Conversion) String() string {
	switch c {
	case CollectStreamToDocument:
		return "collect_stream_to_batch"
	case SynthesizeDocumentToStream:
		return "synthesize_batch_to_stream"
	default:
		return "passthrough"
	}
}

func DeriveConversion(client, provider Delivery) Conversion {
	if client.Mode == Buffered && provider.Mode == Streaming {
		return CollectStreamToDocument
	}
	if client.Mode == Streaming && provider.Mode == Buffered {
		return SynthesizeDocumentToStream
	}
	return Passthrough
}
