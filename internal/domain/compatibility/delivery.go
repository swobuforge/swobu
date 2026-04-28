package compatibility

type DeliveryMode string

const (
	DeliveryModeBuffered  DeliveryMode = "buffered"
	DeliveryModeStreaming DeliveryMode = "streaming"
)

func DeliveryModeFromStreamingRequested(streamingRequested bool) DeliveryMode {
	if streamingRequested {
		return DeliveryModeStreaming
	}
	return DeliveryModeBuffered
}
