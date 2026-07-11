package carrier

import (
	"context"
	"io"
)

type FrameKind string

const (
	FramePayload FrameKind = "payload"
	FrameComment FrameKind = "comment"
	FrameControl FrameKind = "control"
	FrameError   FrameKind = "error"
)

type Frame struct {
	Seq  uint64
	Kind FrameKind
	Name string
	Data []byte
	Meta Meta
}

type FrameReader interface {
	Next(context.Context) (Frame, error)
	Close() error
}

func FrameReaderFromReadCloser(body io.ReadCloser) FrameReader {
	if body == nil {
		return nil
	}
	return &chunkReadCloserFrameReader{body: body}
}

func ReadCloserFromFrameReader(frames FrameReader) io.ReadCloser {
	if frames == nil {
		return nil
	}
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		defer func() { _ = frames.Close() }()
		for {
			frame, err := frames.Next(context.Background())
			if err != nil {
				if err == io.EOF {
					return
				}
				_ = pw.CloseWithError(err)
				return
			}
			if len(frame.Data) == 0 {
				continue
			}
			if _, err := pw.Write(frame.Data); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
	}()
	return pr
}

type chunkReadCloserFrameReader struct {
	body io.ReadCloser
	seq  uint64
}

func (r *chunkReadCloserFrameReader) Next(context.Context) (Frame, error) {
	buf := make([]byte, 4096)
	n, err := r.body.Read(buf)
	if n > 0 {
		frame := Frame{
			Seq:  r.seq,
			Kind: FramePayload,
			Data: append([]byte(nil), buf[:n]...),
		}
		r.seq++
		return frame, nil
	}
	if err != nil {
		return Frame{}, err
	}
	return Frame{}, io.EOF
}

func (r *chunkReadCloserFrameReader) Close() error { return r.body.Close() }
