package protocols

import (
	"bufio"
	"io"
	"strings"
)

type SSEEvent struct {
	Event string
	Data  string
}

type SSEReaderCloser struct {
	scanner *bufio.Scanner
	body    io.ReadCloser
}

func NewSSEReader(body io.ReadCloser) *SSEReaderCloser {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &SSEReaderCloser{
		scanner: scanner,
		body:    body,
	}
}

func (r *SSEReaderCloser) Next() (SSEEvent, error) {
	var eventName string
	var data []string
	for r.scanner.Scan() {
		line := r.scanner.Text()
		if line == "" {
			if len(data) == 0 {
				continue
			}
			return SSEEvent{
				Event: eventName,
				Data:  strings.Join(data, "\n"),
			}, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := r.scanner.Err(); err != nil {
		return SSEEvent{}, err
	}
	if len(data) == 0 {
		return SSEEvent{}, io.EOF
	}
	return SSEEvent{
		Event: eventName,
		Data:  strings.Join(data, "\n"),
	}, nil
}

func (r *SSEReaderCloser) Close() error {
	return r.body.Close()
}
