package httpcodec

func SSEData(raw []byte) []byte {
	return append(append([]byte("data: "), raw...), []byte("\n\n")...)
}

func SSEEventFrame(event string, raw []byte) []byte {
	frame := make([]byte, 0, len(event)+len(raw)+16)
	frame = append(frame, []byte("event: ")...)
	frame = append(frame, event...)
	frame = append(frame, '\n')
	frame = append(frame, []byte("data: ")...)
	frame = append(frame, raw...)
	frame = append(frame, '\n', '\n')
	return frame
}
