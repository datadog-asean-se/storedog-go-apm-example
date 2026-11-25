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
		switch {
		case scenario < 30: // N+1 product listing
			req, _ = http.NewRequest("GET", target+"/api/products", nil)
		case scenario < 40: // same listing, fixed query
			req, _ = http.NewRequest("GET", target+"/api/products?optimized=true", nil)
		case scenario < 55:
			terms := []string{"item 1", "item 2", "storedog", "xyz"}
			req, _ = http.NewRequest("GET", target+"/api/search?q="+terms[rand.Intn(len(terms))], nil)
		case scenario < 93:
			req, _ = http.NewRequest("POST", target+"/api/checkout", bytes.NewReader(cart()))
			req.Header.Set("Content-Type", "application/json")
		case scenario < 97: // bad SKU, 409 from inventory
			req, _ = http.NewRequest("POST", target+"/api/checkout", bytes.NewReader([]byte(`{"tier":"free","items":[{"sku":"SKU-999","qty":1}]}`)))
		default: // panic path for Error Tracking
			req, _ = http.NewRequest("GET", target+"/api/panic", nil)
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
