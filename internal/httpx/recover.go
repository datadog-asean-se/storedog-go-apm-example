// Package httpx holds small HTTP helpers shared by the services.
package httpx

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/DataDog/dd-trace-go/v2/ddtrace/ext"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"

	"github.com/datadog-asean-se/storedog-go-apm-example/internal/telemetry"
)

// Handle registers h on mux with panic recovery applied inside the traced request, so the
// request span (created by the Orchestrion-instrumented mux) is still in the context.
func Handle(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	mux.Handle(pattern, Recoverer(h))
}

// Recoverer turns a panic into an HTTP 500 and records it on the request span with the
// real error type and the full stack. Datadog Error
// Tracking groups on type, message and stack, so each distinct bug becomes its own issue.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				err, isErr := rec.(error)
				if !isErr {
					err = fmt.Errorf("%v", rec)
				}
				telemetry.LogError(r.Context(), "panic recovered", err, "error.stack", string(debug.Stack()), "http.method", r.Method, "http.url", r.URL.Path)
				if span, ok := tracer.SpanFromContext(r.Context()); ok {
					// The HTTP integration overwrites error.type and error.message with a generic
					// "500: Internal Server Error" when it finishes the span. Finishing the span
					// here, with the real error, keeps the real type (for example
					// runtime.boundsError) and message so Error Tracking can group each bug.
					span.SetTag(ext.HTTPCode, "500")
					span.SetTag(ext.ErrorStack, string(debug.Stack()))
					span.Finish(tracer.WithError(err))
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
