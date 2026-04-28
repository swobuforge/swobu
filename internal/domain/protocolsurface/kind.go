package protocolsurface

import "fmt"

// Kind names one concrete protocol surface used at Swobu's edges.
type Kind string

const (
	ChatCompletions Kind = "chat_completions"
	Responses       Kind = "responses"
	Completions     Kind = "completions"
	Messages        Kind = "messages"
)

func (k Kind) String() string {
	return string(k)
}

func Parse(raw string) (Kind, error) {
	switch Kind(raw) {
	case ChatCompletions, Responses, Completions, Messages:
		return Kind(raw), nil
	default:
		return "", fmt.Errorf("unsupported protocol surface %q", raw)
	}
}

func (k Kind) MarshalText() ([]byte, error) {
	if _, err := Parse(k.String()); err != nil {
		return nil, err
	}
	return []byte(k), nil
}

func (k *Kind) UnmarshalText(text []byte) error {
	parsed, err := Parse(string(text))
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}
