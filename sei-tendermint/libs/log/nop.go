package log

import (
	"context"
	"log/slog"
)

type noopHandler struct {
	attrs []slog.Attr
	group string
}

func (h *noopHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (h *noopHandler) Handle(context.Context, slog.Record) error { return nil }
func (h *noopHandler) WithAttrs([]slog.Attr) slog.Handler        { return &noopHandler{} }
func (h *noopHandler) WithGroup(string) slog.Handler             { return &noopHandler{} }

func NewNopLogger() Logger {
	return &defaultLogger{logger: slog.New(&noopHandler{})}
}
