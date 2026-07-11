package protocolsurface

// CodecID names one explicit family+delivery+framing combination at the
// adapter edge.
type CodecID string

const (
	CodecIDChatCompletionsBufferedJSON CodecID = "chat_completions.buffered.json"
	CodecIDChatCompletionsStreamSSE    CodecID = "chat_completions.streaming.sse"
	CodecIDResponsesBufferedJSON       CodecID = "responses.buffered.json"
	CodecIDResponsesStreamSSE          CodecID = "responses.streaming.sse"
	CodecIDCompletionsBufferedJSON     CodecID = "completions.buffered.json"
	CodecIDCompletionsStreamSSE        CodecID = "completions.streaming.sse"
	CodecIDMessagesBufferedJSON        CodecID = "messages.buffered.json"
	CodecIDMessagesStreamSSE           CodecID = "messages.streaming.sse"
)

func (id CodecID) String() string {
	return string(id)
}
