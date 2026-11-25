// Package loadgen sends a steady, mixed stream of requests to the gateway so
// traces appear in Datadog right after deploy, with no manual clicking.
package loadgen

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"
)

func Run(log *slog.Logger) {
	target := os.Getenv("TARGET_URL")
	if target == "" {
		target = "http://shop-gateway:8080"
	}
	interval := 1500 * time.Millisecond
	if d, err := time.ParseDuration(os.Getenv("LOAD_INTERVAL")); err == nil {
		interval = d
	}
	// The loadgen is traced too, so each synthetic user journey is one full trace
	// starting at go-shop-loadgen.
	client := &http.Client{Timeout: 10 * time.Second}
	log.Info("loadgen started", "target", target, "interval", interval)

	for range time.Tick(interval) {
		scenario := rand.Intn(100)
		var req *http.Request
		post := func(body string) *http.Request {
			r, _ := http.NewRequest("POST", target+"/api/checkout", bytes.NewReader([]byte(body)))
			r.Header.Set("Content-Type", "application/json")
			return r
		}
		get := func(path string) *http.Request { r, _ := http.NewRequest("GET", target+path, nil); return r }
		switch {
		case scenario < 25: // N+1 product listing
			req = get("/api/products")
		case scenario < 33: // same listing, fixed query
			req = get("/api/products?optimized=true")
		case scenario < 45:
			terms := []string{"item 1", "item 2", "storedog", "xyz"}
			req = get("/api/search?q=" + terms[rand.Intn(len(terms))])
		case scenario < 80:
			req = post(string(cart()))
		// Planted bugs for Error Tracking, each about 2 to 3% of traffic.
		case scenario < 82: // unknown SKU -> UnknownSKUError
			req = post(`{"tier":"free","items":[{"sku":"SKU-999","qty":1}]}`)
		case scenario < 85: // too many units -> InsufficientStockError
			req = post(`{"tier":"pro","items":[{"sku":"SKU-001","qty":500}]}`)
		case scenario < 87: // bad JSON type -> *json.UnmarshalTypeError
			req = post(`{"tier":"pro","items":[{"sku":"SKU-001","qty":"two"}]}`)
		case scenario < 90:
			req = get("/api/customer?id=c-999") // nil pointer dereference
		case scenario < 93:
			req = get("/api/recommendations?sku=SKU-001") // index out of range
		case scenario < 96:
			req = get("/api/discount?total=100&qty=0") // integer divide by zero
		case scenario < 98:
			req = get("/api/meta") // interface conversion (panics in inventory)
		default:
			req = get("/api/panic") // nil map write
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Warn("request failed", "err", err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func cart() []byte {
	n := 1 + rand.Intn(6)
	items := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			items += ","
		}
		items += fmt.Sprintf(`{"sku":"SKU-%03d","qty":%d}`, 1+rand.Intn(40), 1+rand.Intn(8))
	}
	tiers := []string{"free", "pro", "enterprise"}
	return []byte(fmt.Sprintf(`{"tier":%q,"items":[%s]}`, tiers[rand.Intn(3)], items))
}
