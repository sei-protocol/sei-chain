package traceprofile

import (
	"context"
	"time"
)

type Recorder interface {
	AddCount(name string, delta int)
	AddDuration(name string, duration time.Duration)
}

type contextKey struct{}

func WithRecorder(ctx context.Context, recorder Recorder) context.Context {
	return context.WithValue(ctx, contextKey{}, recorder)
}

func FromContext(ctx context.Context) Recorder {
	recorder, _ := ctx.Value(contextKey{}).(Recorder)
	return recorder
}
