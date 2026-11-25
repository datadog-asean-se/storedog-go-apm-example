// Package telemetry builds the JSON logger. Trace and log correlation needs no
// code: Orchestrion instruments log/slog at build time, so any call made with a
// context (log.InfoContext(ctx, ...)) gets dd.trace_id and dd.span_id added.
package telemetry

import (
	"log/slog"
	"os"
)

// Logger returns a JSON slog logger tagged with Unified Service Tagging fields.
func Logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", os.Getenv("DD_SERVICE"),
		"env", os.Getenv("DD_ENV"), "version", os.Getenv("DD_VERSION"))
}
