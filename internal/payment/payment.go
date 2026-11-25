// Package payment simulates a card-charging service with retries, errors and a
// CPU-heavy fraud check (handy for the continuous profiler).
package payment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/DataDog/dd-trace-go/v2/ddtrace/ext"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
)

var errDeclined = errors.New("card declined by issuer")

func Run(addr string, log *slog.Logger) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/charge", func(w http.ResponseWriter, r *http.Request) { charge(w, r, log) })
	log.Info("payment listening", "addr", addr)
	return http.ListenAndServe(addr, mux)
}

func charge(w http.ResponseWriter, r *http.Request, log *slog.Logger) {
	var req struct {
		Amount float64 `json:"amount"`
		Tier   string  `json:"tier"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	ctx := r.Context()

	root, _ := tracer.SpanFromContext(ctx)
	if root != nil { // enrich the span created by the HTTP contrib
		root.SetTag("payment.amount", req.Amount)
		root.SetTag("customer.tier", req.Tier)
	}

	fraudCheck(ctx, req.Amount)

	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		err = callAcquirer(ctx, attempt)
		if err == nil {
			break
		}
		log.WarnContext(ctx, "acquirer attempt failed", "attempt", attempt, "err", err)
	}
	if err != nil {
		if root != nil {
			root.SetTag(ext.Error, err)
		}
		http.Error(w, err.Error(), http.StatusPaymentRequired)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"status": "approved", "auth": fmt.Sprintf("AUTH-%06d", rand.Intn(1e6))})
}

// fraudCheck burns CPU with repeated hashing; look for it in the profiler flame graph.
func fraudCheck(ctx context.Context, amount float64) {
	span, _ := tracer.StartSpanFromContext(ctx, "payment.fraud_check", tracer.ResourceName("score transaction"))
	defer span.Finish()
	rounds := 20000
	if amount > 100 {
		rounds = 120000 // big baskets get a deeper check
	}
	sum := sha256.Sum256([]byte("seed"))
	for i := 0; i < rounds; i++ {
		sum = sha256.Sum256(sum[:])
	}
	span.SetTag("fraud.rounds", rounds)
	span.SetTag("fraud.score", int(sum[0])%100)
}

// callAcquirer simulates the external card network, with jittery latency and ~25% failures.
func callAcquirer(ctx context.Context, attempt int) error {
	span, _ := tracer.StartSpanFromContext(ctx, "acquirer.authorize", tracer.ServiceName("card-network"),
		tracer.ResourceName("POST /authorize"), tracer.SpanType(ext.SpanTypeHTTP))
	span.SetTag("retry.attempt", attempt)
	time.Sleep(time.Duration(20+rand.Intn(120)) * time.Millisecond)
	var err error
	if rand.Intn(4) == 0 {
		err = errDeclined
	}
	span.Finish(tracer.WithError(err))
	return err
}
