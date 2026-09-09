package core

import (
	"context"
	"io"
	"strings"
	"testing"
)

// sseBody builds frameCount valid SSE frames that the scanner decodes into the
// same number of events. Payloads are kept tiny and constant so the benchmark
// measures reader/next overhead, not payload allocation.
func sseBody(frameCount int) string {
	var b strings.Builder
	for i := range frameCount {
		b.WriteString("event: message\n")
		b.WriteString("data: {\"i\":")
		b.WriteByte('0' + byte(i%10))
		b.WriteString("}\n\n")
	}
	return b.String()
}

func BenchmarkSSEReaderNext(b *testing.B) {
	for _, frameCount := range []int{16, 512, 4096} {
		body := sseBody(frameCount)
		b.Run(nameForSSEScale(frameCount), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				reader := NewSSEReader(io.NopCloser(strings.NewReader(body)))
				ctx := context.Background()
				for {
					if _, err := reader.Next(ctx); err != nil {
						break
					}
				}
			}
		})
	}
}

func nameForSSEScale(n int) string {
	switch n {
	case 16:
		return "Short"
	case 512:
		return "Medium"
	default:
		return "Long"
	}
}
