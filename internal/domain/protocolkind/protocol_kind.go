package protocolkind

import "fmt"

// ProtocolKind names one concrete protocol family used at Swobu edges.
type ProtocolKind string

const (
	ChatCompletions ProtocolKind = "chat_completions"
	Responses       ProtocolKind = "responses"
	Completions     ProtocolKind = "completions"
	Messages        ProtocolKind = "messages"
)

func (k ProtocolKind) String() string {
	return string(k)
}

func ParseProtocolKind(raw string) (ProtocolKind, error) {
	switch ProtocolKind(raw) {
	case ChatCompletions, Responses, Completions, Messages:
		return ProtocolKind(raw), nil
	default:
		return "", fmt.Errorf("unsupported protocol kind %q", raw)
	}
}

func (k ProtocolKind) MarshalText() ([]byte, error) {
	if _, err := ParseProtocolKind(k.String()); err != nil {
		return nil, err
	}
	return []byte(k), nil
}

func (k *ProtocolKind) UnmarshalText(text []byte) error {
	parsed, err := ParseProtocolKind(string(text))
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}
