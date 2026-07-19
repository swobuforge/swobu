package core

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
)

type SSEEvent struct {
	Event string
	Data  string
}

type SSEReaderCloser struct {
	scanner   *bufio.Scanner
	body      io.ReadCloser
	closeOnce sync.Once
	closeErr  error
}

func NewSSEReader(body io.ReadCloser) *SSEReaderCloser {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &SSEReaderCloser{
		scanner: scanner,
		body:    body,
	}
}

func (r *SSEReaderCloser) Next(ctx context.Context) (SSEEvent, error) {
	if err := ctx.Err(); err != nil {
		return SSEEvent{}, err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = r.Close()
		case <-done:
		}
	}()
	event, err := r.next()
	close(done)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return SSEEvent{}, ctxErr
	}
	return event, err
}

func (r *SSEReaderCloser) next() (SSEEvent, error) {
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
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:")) // swobu:io-string source=boundary
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:"))) // swobu:io-string source=boundary
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
	if r == nil || r.body == nil {
		return nil
	}
	r.closeOnce.Do(func() { r.closeErr = r.body.Close() })
	return r.closeErr
}
