// Package telemetry builds the JSON logger. Trace and log correlation needs no
// code: Orchestrion instruments log/slog at build time, so any call made with a
// context (log.InfoContext(ctx, ...)) gets dd.trace_id and dd.span_id added.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// Logger returns a JSON slog logger tagged with Unified Service Tagging fields.
func Logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", os.Getenv("DD_SERVICE"),
		"env", os.Getenv("DD_ENV"), "version", os.Getenv("DD_VERSION"))
}

// LogError writes an ERROR log tied to the active trace. Use it wherever an error is recorded
// on a span, so the log line and the trace link to each other. Pass the context of the request:
// with it, Orchestrion adds dd.trace_id and dd.span_id. The error.* names are Datadog's
// standard log attributes.
func LogError(ctx context.Context, msg string, err error, args ...any) {
	base := []any{"error.kind", fmt.Sprintf("%T", err), "error.message", err.Error()}
	slog.ErrorContext(ctx, msg, append(base, args...)...)
}
