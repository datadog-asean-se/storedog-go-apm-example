// Package inventory is the product catalogue service, backed by in-memory SQLite
// through database/sql so every query appears as a span in the trace.
package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/DataDog/dd-trace-go/v2/ddtrace/ext"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	_ "modernc.org/sqlite"

	"github.com/datadog-asean-se/storedog-go-apm-example/internal/httpx"
	"github.com/datadog-asean-se/storedog-go-apm-example/internal/telemetry"
)

type Product struct {
	SKU   string  `json:"sku"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

func Run(addr string, log *slog.Logger) error {
	// Plain database/sql: Orchestrion wraps it at build time, so every query
	// becomes a span without any tracing code here.
	db, err := sql.Open("sqlite", "file:inventory?mode=memory&cache=shared")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1) // in-memory sqlite: keep a single connection
	if err := seed(db); err != nil {
		return err
	}
	s := &server{db: db, log: log}
	go s.syncLoop()

	mux := http.NewServeMux()
	httpx.Handle(mux, "/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	httpx.Handle(mux, "/products", s.products)
	httpx.Handle(mux, "/stock", s.stock)
	httpx.Handle(mux, "/search", s.search)
	httpx.Handle(mux, "/products/meta", s.meta)
	log.Info("inventory listening", "addr", addr)
	return http.ListenAndServe(addr, mux)
}

type server struct {
	db  *sql.DB
	log *slog.Logger
}

func seed(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE products (sku TEXT PRIMARY KEY, name TEXT, price REAL)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE stock (sku TEXT PRIMARY KEY, qty INTEGER)`); err != nil {
		return err
	}
	for i := 1; i <= 40; i++ {
		sku := fmt.Sprintf("SKU-%03d", i)
		db.Exec(`INSERT INTO products VALUES (?,?,?)`, sku, fmt.Sprintf("Storedog item %d", i), 5+float64(i)*1.25)
		db.Exec(`INSERT INTO stock VALUES (?,?)`, sku, 50+i)
	}
	return nil
}

// products demonstrates the classic N+1 query pattern. With ?optimized=true it
// uses a single JOIN instead, so you can compare the two traces side by side.
func (s *server) products(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	span, ctx := tracer.StartSpanFromContext(ctx, "inventory.list_products", tracer.ResourceName("list products"))
	defer span.Finish()

	optimized := r.URL.Query().Get("optimized") == "true"
	span.SetTag("query.optimized", optimized)
	var out []Product
	if optimized {
		rows, err := s.db.QueryContext(ctx, `SELECT p.sku, p.name, p.price, s.qty FROM products p JOIN stock s ON s.sku = p.sku LIMIT 20`)
		if err != nil {
			fail(w, span, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p Product
			rows.Scan(&p.SKU, &p.Name, &p.Price, &p.Stock)
			out = append(out, p)
		}
	} else {
		rows, err := s.db.QueryContext(ctx, `SELECT sku, name, price FROM products LIMIT 20`)
		if err != nil {
			fail(w, span, err)
			return
		}
		var ps []Product
		for rows.Next() {
			var p Product
			rows.Scan(&p.SKU, &p.Name, &p.Price)
			ps = append(ps, p)
		}
		rows.Close()
		for _, p := range ps { // N more queries, one per product
			s.db.QueryRowContext(ctx, `SELECT qty FROM stock WHERE sku = ?`, p.SKU).Scan(&p.Stock)
			out = append(out, p)
		}
	}
	span.SetTag("products.count", len(out))
	writeJSON(w, out)
}

func (s *server) stock(w http.ResponseWriter, r *http.Request) {
	sku := r.URL.Query().Get("sku")
	span, ctx := tracer.StartSpanFromContext(r.Context(), "inventory.check_stock", tracer.ResourceName("check stock"))
	defer span.Finish()
	span.SetTag("product.sku", sku)

	var qty int
	err := s.db.QueryRowContext(ctx, `SELECT qty FROM stock WHERE sku = ?`, sku).Scan(&qty)
	if err == sql.ErrNoRows {
		span.SetTag(ext.ErrorMsg, "unknown sku")
		http.Error(w, "unknown sku "+sku, http.StatusNotFound)
		return
	} else if err != nil {
		fail(w, span, err)
		return
	}
	span.SetTag("stock.qty", qty)
	s.log.InfoContext(ctx, "stock checked", "sku", sku, "qty", qty)
	writeJSON(w, map[string]any{"sku": sku, "qty": qty})
}

// search runs an unindexed LIKE scan; a rare slow path adds an artificial delay.
func (s *server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	span, ctx := tracer.StartSpanFromContext(r.Context(), "inventory.search", tracer.ResourceName("search products"))
	defer span.Finish()
	span.SetTag("search.term", q)
	if rand.Intn(10) == 0 {
		time.Sleep(time.Duration(300+rand.Intn(400)) * time.Millisecond) // simulated lock wait
		span.SetTag("search.slow_path", true)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sku, name, price FROM products WHERE name LIKE ?`, "%"+strings.ReplaceAll(q, "%", "")+"%")
	if err != nil {
		fail(w, span, err)
		return
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		var p Product
		rows.Scan(&p.SKU, &p.Name, &p.Price)
		out = append(out, p)
	}
	writeJSON(w, out)
}

// syncLoop is a background worker. Each run starts its own root trace so you can
// see non-HTTP work (cron, queue consumers) in APM too.
func (s *server) syncLoop() {
	for range time.Tick(30 * time.Second) {
		span := tracer.StartSpan("inventory.sync", tracer.ServiceName("go-shop-inventory"),
			tracer.ResourceName("nightly stock sync"), tracer.SpanType(ext.SpanTypeWorker))
		ctx := tracer.ContextWithSpan(context.Background(), span)
		res, err := s.db.ExecContext(ctx, `UPDATE stock SET qty = qty + 1 WHERE qty < 200`)
		if err == nil {
			n, _ := res.RowsAffected()
			span.SetTag("rows.updated", n)
		}
		span.Finish(tracer.WithError(err))
	}
}

func fail(w http.ResponseWriter, span *tracer.Span, err error) {
	span.SetTag(ext.Error, err)
	telemetry.LogError(tracer.ContextWithSpan(context.Background(), span), "inventory query failed", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// demo bug 5: wrong type assertion. JSON numbers decode to float64, but the code asserts int,
// so this panics with "interface conversion: interface {} is float64, not int".
func (s *server) meta(w http.ResponseWriter, r *http.Request) {
	var m map[string]any
	json.Unmarshal([]byte(`{"weight_g": 120, "warranty_months": 12}`), &m)
	weight := m["weight_g"].(int)
	writeJSON(w, map[string]int{"weight_g": weight})
}
