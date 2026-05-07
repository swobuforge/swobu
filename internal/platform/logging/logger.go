package logging

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const envLogLevel = "SWOBU_LOG_LEVEL"

// ConfigureDefaultLogger installs a process-wide logger with stable
// human-readable line formatting while preserving file:line source context.
func ConfigureDefaultLogger(out io.Writer) {
	logger := slog.New(NewCommonLineHandler(out, configuredLogLevel()))
	slog.SetDefault(logger)
	log.SetFlags(0)
	log.SetOutput(newStdlibLogBridge(logger))
}

func configuredLogLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envLogLevel))) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// CommonLineHandler formats records as:
// 2026-05-06T14:32:18.421Z INFO  run.go:171 daemon lifecycle key=value
type CommonLineHandler struct {
	out    io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
	mu     *sync.Mutex
}

func NewCommonLineHandler(out io.Writer, level slog.Leveler) *CommonLineHandler {
	if level == nil {
		level = slog.LevelInfo
	}
	return &CommonLineHandler{
		out:   out,
		level: level,
		mu:    &sync.Mutex{},
	}
}

func (h *CommonLineHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *CommonLineHandler) Handle(_ context.Context, r slog.Record) error {
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}

	var b strings.Builder
	b.WriteString(ts.UTC().Format("2006-01-02T15:04:05.000Z"))
	b.WriteByte(' ')
	b.WriteString(paddedLevel(r.Level))
	b.WriteByte(' ')
	b.WriteString(sourceString(r))
	msg := strings.TrimSpace(r.Message)
	if msg != "" && msg != "daemon lifecycle" {
		b.WriteByte(' ')
		b.WriteString(msg)
	}

	attrs := make([]slog.Attr, 0, len(h.attrs)+8)
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	for _, a := range attrs {
		if a.Equal(slog.Attr{}) {
			continue
		}
		key := qualifyKey(h.groups, a.Key)
		if key == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(formatAttrValue(a.Value))
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, b.String())
	return err
}

func (h *CommonLineHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *CommonLineHandler) WithGroup(name string) slog.Handler {
	next := *h
	if strings.TrimSpace(name) != "" {
		next.groups = append(append([]string{}, h.groups...), strings.TrimSpace(name))
	}
	return &next
}

func paddedLevel(level slog.Level) string {
	switch {
	case level <= slog.LevelDebug:
		return "DEBUG"
	case level < slog.LevelWarn:
		return "INFO "
	case level < slog.LevelError:
		return "WARN "
	default:
		return "ERROR"
	}
}

func sourceString(r slog.Record) string {
	if r.PC == 0 {
		return "-:-"
	}
	frames := runtime.CallersFrames([]uintptr{r.PC})
	frame, _ := frames.Next()
	if strings.TrimSpace(frame.File) == "" || frame.Line <= 0 {
		return "-:-"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(frame.File), frame.Line)
}

func qualifyKey(groups []string, key string) string {
	k := strings.TrimSpace(key)
	if k == "" {
		return ""
	}
	if len(groups) == 0 {
		return k
	}
	return strings.Join(append(append([]string{}, groups...), k), ".")
}

func formatAttrValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if strings.ContainsAny(s, " \t\n\r=\"") {
			return strconv.Quote(s)
		}
		return s
	case slog.KindInt64:
		return strconv.FormatInt(v.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(v.Float64(), 'f', -1, 64)
	case slog.KindBool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().UTC().Format("2006-01-02T15:04:05.000Z")
	default:
		s := v.String()
		if strings.ContainsAny(s, " \t\n\r=\"") {
			return strconv.Quote(s)
		}
		return s
	}
}

var stdlibPrefixRE = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}\s+`)

type stdlibLogBridge struct {
	logger *slog.Logger
	mu     sync.Mutex
	buf    strings.Builder
}

func newStdlibLogBridge(logger *slog.Logger) *stdlibLogBridge {
	return &stdlibLogBridge{logger: logger}
}

func (w *stdlibLogBridge) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(p)
	for {
		s := w.buf.String()
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(s[:i])
		rest := s[i+1:]
		w.buf.Reset()
		w.buf.WriteString(rest)
		w.emit(line)
	}
	return len(p), nil
}

func (w *stdlibLogBridge) emit(line string) {
	msg := strings.TrimSpace(stdlibPrefixRE.ReplaceAllString(line, ""))
	if msg == "" {
		return
	}
	w.logger.Warn(msg, "component", "stdlib")
}
