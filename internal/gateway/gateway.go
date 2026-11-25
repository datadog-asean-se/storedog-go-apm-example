// Package gateway is the public entry point. It fans out to inventory and
// payment over HTTP, so a single request becomes one distributed trace.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/DataDog/dd-trace-go/v2/ddtrace/ext"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"

	"github.com/datadog-asean-se/storedog-go-apm-example/internal/httpx"
)

type gw struct {
	inventoryURL, paymentURL string
	client                   *http.Client
	log                      *slog.Logger
}

func Run(addr string, log *slog.Logger) error {
	g := &gw{
		inventoryURL: env("INVENTORY_URL", "http://shop-inventory:8080"),
		paymentURL:   env("PAYMENT_URL", "http://shop-payment:8080"),
		// Orchestrion instruments http.Client at build time and injects the trace
		// headers, so downstream spans join this trace. No wrapper needed.
		client: &http.Client{Timeout: 5 * time.Second},
		log:    log,
	}
	mux := http.NewServeMux()
	httpx.Handle(mux, "/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	f := faultsFromEnv()
	log.Info("gateway fault mode", "mode", f.mode, "rate", f.rate)
	httpx.Handle(mux, "/api/products", f.wrap(g.products))
	httpx.Handle(mux, "/api/search", f.wrap(g.search))
	httpx.Handle(mux, "/api/checkout", f.wrap(g.checkout))
	httpx.Handle(mux, "/api/meta", func(w http.ResponseWriter, r *http.Request) { g.proxy(w, r, g.inventoryURL+"/products/meta") })
	httpx.Handle(mux, "/api/customer", g.customer)
	httpx.Handle(mux, "/api/recommendations", g.recommendations)
	httpx.Handle(mux, "/api/discount", g.discount)
	httpx.Handle(mux, "/api/panic", g.boom)
	log.Info("gateway listening", "addr", addr)
	return http.ListenAndServe(addr, mux)
}

func (g *gw) products(w http.ResponseWriter, r *http.Request) {
	g.proxy(w, r, g.inventoryURL+"/products?"+r.URL.RawQuery)
}

func (g *gw) search(w http.ResponseWriter, r *http.Request) {
	g.proxy(w, r, g.inventoryURL+"/search?"+r.URL.RawQuery)
}

func (g *gw) proxy(w http.ResponseWriter, r *http.Request, url string) {
	body, status, err := g.get(r.Context(), url)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

type cartItem struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

// checkout checks stock for each item in parallel (sibling spans), then charges.
func (g *gw) checkout(w http.ResponseWriter, r *http.Request) {
	var cart struct {
		Items []cartItem `json:"items"`
		Tier  string     `json:"tier"`
	}
	span, ctx := tracer.StartSpanFromContext(r.Context(), "checkout.process", tracer.ResourceName("process cart"))
	defer span.Finish()
	if err := json.NewDecoder(r.Body).Decode(&cart); err != nil {
		span.SetTag(ext.Error, err) // e.g. *json.UnmarshalTypeError
		http.Error(w, "bad cart: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(cart.Items) == 0 {
		http.Error(w, "empty cart", http.StatusBadRequest)
		return
	}
	span.SetTag("cart.items", len(cart.Items))
	span.SetTag("customer.tier", cart.Tier)

	// Parallel stock checks.
	var wg sync.WaitGroup
	errs := make(chan error, len(cart.Items))
	for _, it := range cart.Items {
		wg.Add(1)
		go func(it cartItem) {
			defer wg.Done()
			body, status, err := g.get(ctx, fmt.Sprintf("%s/stock?sku=%s", g.inventoryURL, it.SKU))
			switch {
			case err != nil:
			case status == http.StatusNotFound:
				err = &UnknownSKUError{SKU: it.SKU}
			case status == http.StatusOK:
				var st struct{ Qty int }
				json.Unmarshal(body, &st)
				if st.Qty < it.Qty {
					err = &InsufficientStockError{SKU: it.SKU, Want: it.Qty, Have: st.Qty}
				}
			default:
				err = fmt.Errorf("inventory returned %d", status)
			}
			errs <- err
		}(it)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			span.SetTag(ext.Error, err)
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
	}

	total := 0.0
	for _, it := range cart.Items {
		total += float64(it.Qty) * 12.5
	}
	span.SetTag("order.value", total)
	g.log.InfoContext(ctx, "charging order", "total", total, "items", len(cart.Items))

	payload, _ := json.Marshal(map[string]any{"amount": total, "tier": cart.Tier})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, g.paymentURL+"/charge", bytesReader(payload))
	resp, err := g.client.Do(req)
	if err != nil {
		span.SetTag(ext.Error, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		span.SetTag(ext.Error, &PaymentFailedError{Status: resp.StatusCode, Body: string(out)})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(out)
}

func (g *gw) boom(w http.ResponseWriter, r *http.Request) {
	var m map[string]int
	m["x"] = 1 // nil map write: panics, caught by recoverer, shows up in Error Tracking
}

func (g *gw) get(ctx context.Context, url string) ([]byte, int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
