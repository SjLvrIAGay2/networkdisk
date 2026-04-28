package logging

import "context"

type traceIDKey struct{}

func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, id)
}

func TraceID(ctx context.Context) string {
	v, _ := ctx.Value(traceIDKey{}).(string)
	return v
}
