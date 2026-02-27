package log

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const moduleKey = "module"

// bufPool recycles byte buffers for log line formatting.
var bufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 256)
		return &b
	},
}

// NewTMLogger returns a logger that encodes msg and keyvals to the Writer
// using go-kit's log as an underlying logger and our custom formatter. Note
// that underlying logger could be swapped with something else.
func NewTMLogger(w io.Writer) Logger {
	return &defaultLogger{
		logger: slog.New(NewTMFmtHandler(w, nil)),
	}
}

// TMFmtHandler is an slog.Handler that writes log records in Tendermint's
// custom format:
//
//	D[2016-05-02|11:06:44.322] Stopping AddrBook                            module=main key=value
//
// It is safe for concurrent use.
type TMFmtHandler struct {
	w      io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

// NewTMFmtHandler returns a new TMFmtHandler that writes to w.
// opts may be nil for defaults.
func NewTMFmtHandler(w io.Writer, opts *slog.HandlerOptions) *TMFmtHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	level := opts.Level
	if level == nil {
		level = slog.LevelInfo
	}
	return &TMFmtHandler{
		w:     w,
		level: level,
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *TMFmtHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle formats and writes a log record.
func (h *TMFmtHandler) Handle(_ context.Context, r slog.Record) error {
	bp := bufPool.Get().(*[]byte)
	buf := (*bp)[:0]
	defer func() {
		*bp = buf
		bufPool.Put(bp)
	}()

	// Timestamp.
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}

	// Header: "D[2006-01-02|15:04:05.000] %-44s "
	buf = append(buf, levelChar(r.Level))
	buf = append(buf, '[')
	buf = ts.AppendFormat(buf, "2006-01-02|15:04:05.000")
	buf = append(buf, ']')
	buf = append(buf, ' ')

	// Left-justify message in 44 chars.
	msg := r.Message
	buf = append(buf, msg...)
	if pad := 44 - len(msg); pad > 0 {
		for i := 0; i < pad; i++ {
			buf = append(buf, ' ')
		}
	}
	buf = append(buf, ' ')

	// Collect key-value pairs, extracting "module" to place it first.
	module := ""
	kvStart := len(buf)

	appendAttr := func(key string, val slog.Value) {
		if key == moduleKey {
			module = val.String()
			return
		}
		buf = appendKeyval(buf, key, val)
	}

	for _, a := range h.attrs {
		appendAttr(h.prefixedKey(a.Key), a.Value)
	}
	r.Attrs(func(a slog.Attr) bool {
		appendAttr(h.prefixedKey(a.Key), a.Value)
		return true
	})

	// Insert "module=..." right after the message, before other key-values.
	if module != "" {
		kvBytes := make([]byte, len(buf)-kvStart)
		copy(kvBytes, buf[kvStart:])
		buf = buf[:kvStart]
		buf = append(buf, "module="...)
		buf = appendVal(buf, module)
		buf = append(buf, ' ')
		buf = append(buf, kvBytes...)
	}

	// Trim trailing space, add newline.
	if len(buf) > 0 && buf[len(buf)-1] == ' ' {
		buf = buf[:len(buf)-1]
	}
	buf = append(buf, '\n')

	_, err := h.w.Write(buf)
	return err
}

// WithAttrs returns a new handler with the given attrs pre-collected.
func (h *TMFmtHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	h2 := h.clone()
	h2.attrs = append(h2.attrs, attrs...)
	return h2
}

// WithGroup returns a new handler where all subsequent attrs are nested under
// the given group name (dot-separated).
func (h *TMFmtHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := h.clone()
	h2.groups = append(h2.groups, name)
	return h2
}

func (h *TMFmtHandler) clone() *TMFmtHandler {
	return &TMFmtHandler{
		w:      h.w,
		level:  h.level,
		attrs:  append([]slog.Attr(nil), h.attrs...),
		groups: append([]string(nil), h.groups...),
	}
}

func (h *TMFmtHandler) prefixedKey(key string) string {
	if len(h.groups) == 0 {
		return key
	}
	return strings.Join(h.groups, ".") + "." + key
}

// levelChar maps an slog.Level to a single uppercase character.
func levelChar(l slog.Level) byte {
	switch {
	case l >= slog.LevelError:
		return 'E'
	case l >= slog.LevelWarn:
		return 'W'
	case l >= slog.LevelInfo:
		return 'I'
	default:
		return 'D'
	}
}

// appendKeyval appends "key=value " to buf in logfmt style.
func appendKeyval(buf []byte, key string, v slog.Value) []byte {
	buf = append(buf, key...)
	buf = append(buf, '=')
	buf = appendValue(buf, v)
	buf = append(buf, ' ')
	return buf
}

// appendValue formats an slog.Value for logfmt output.
func appendValue(buf []byte, v slog.Value) []byte {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return appendVal(buf, v.String())
	case slog.KindInt64:
		return fmt.Appendf(buf, "%d", v.Int64())
	case slog.KindUint64:
		return fmt.Appendf(buf, "%d", v.Uint64())
	case slog.KindFloat64:
		return fmt.Appendf(buf, "%g", v.Float64())
	case slog.KindBool:
		if v.Bool() {
			return append(buf, "true"...)
		}
		return append(buf, "false"...)
	case slog.KindDuration:
		return appendVal(buf, v.Duration().String())
	case slog.KindTime:
		return appendVal(buf, v.Time().Format(time.RFC3339Nano))
	case slog.KindAny:
		a := v.Any()
		// Match original: []byte → uppercase hex string.
		if b, ok := a.([]byte); ok {
			return append(buf, strings.ToUpper(hex.EncodeToString(b))...)
		}
		return appendVal(buf, fmt.Sprintf("%+v", a))
	default:
		return appendVal(buf, fmt.Sprintf("%+v", v.Any()))
	}
}

// appendVal appends s to buf, quoting it if necessary for logfmt.
func appendVal(buf []byte, s string) []byte {
	if s == "" {
		return append(buf, `""`...)
	}
	if needsQuote(s) {
		return fmt.Appendf(buf, "%q", s)
	}
	return append(buf, s...)
}

// needsQuote reports whether s needs quoting for logfmt.
func needsQuote(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '"' || r == '=' || r == '\\' || unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return true
		}
		i += size
	}
	return false
}
