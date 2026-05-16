package canonical

// EventReaderFromCanonicalOutput projects a buffered canonical output into a
// finite canonical envelope event stream.
func EventReaderFromCanonicalOutput(exchangeID string, output CanonicalOutput) (EventReader, error) {
	events, err := SynthesizeResponseFromOutput(exchangeID, output)
	if err != nil {
		return nil, err
	}
	return NewSliceEventReader(events), nil
}
