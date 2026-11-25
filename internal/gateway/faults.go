package gateway

import (
	"errors"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/DataDog/dd-trace-go/v2/ddtrace/ext"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
)

// Fault injection simulates a bad release. The same image runs three "versions" of the
// gateway; only the env vars differ (see deploy/overlays/versions):
//
//	FAULT_MODE=none      healthy (default)
//	FAULT_MODE=errors    a share of requests fail with HTTP 500
//	FAULT_MODE=latency   a share of requests are slow
//	FAULT_RATE=0.3       share of affected requests (0 to 1)
type faults struct {
	mode string
	rate float64
}

func faultsFromEnv() faults {
	f := faults{mode: os.Getenv("FAULT_MODE"), rate: 0.3}
	if v, err := strconv.ParseFloat(os.Getenv("FAULT_RATE"), 64); err == nil {
		f.rate = v
	}
	return f
}

var errRegression = errors.New("pricing lookup failed: upstream returned an unexpected schema (simulated regression)")

// wrap applies the fault to the business endpoints only.
func (f faults) wrap(next http.HandlerFunc) http.HandlerFunc {
	if f.mode == "" || f.mode == "none" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if rand.Float64() >= f.rate {
			next(w, r)
			return
		}
		switch f.mode {
		case "errors":
			span, _ := tracer.StartSpanFromContext(r.Context(), "gateway.pricing_lookup", tracer.ResourceName("lookup prices"))
			span.Finish(tracer.WithError(errRegression))
			if root, ok := tracer.SpanFromContext(r.Context()); ok {
				root.SetTag(ext.Error, errRegression)
			}
			http.Error(w, errRegression.Error(), http.StatusInternalServerError)
			return
		case "latency":
			span, _ := tracer.StartSpanFromContext(r.Context(), "gateway.legacy_pricing_lookup", tracer.ResourceName("lookup prices (no cache)"))
			d := time.Duration(400+rand.Intn(900)) * time.Millisecond
			if rand.Intn(10) == 0 {
				d = 2500 * time.Millisecond
			}
			span.SetTag("delay.ms", d.Milliseconds())
			time.Sleep(d)
			span.Finish()
		}
		next(w, r)
	}
}
