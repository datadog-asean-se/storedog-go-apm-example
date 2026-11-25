package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// The handlers below contain deliberate bugs ("demo bugs"). Each one fails in a different way,
// so Datadog Error Tracking shows them as separate issues. They are meant for showing
// Error Tracking and Bits Code (AI-suggested fixes), not as good code. See the README.

type Customer struct {
	ID   string
	Tier string
}

var customers = map[string]*Customer{
	"c-100": {ID: "c-100", Tier: "pro"},
	"c-200": {ID: "c-200", Tier: "enterprise"},
}

// demo bug 1: nil pointer dereference. Unknown customer IDs return nil and the handler
// reads c.Tier without checking.
func (g *gw) customer(w http.ResponseWriter, r *http.Request) {
	c := customers[r.URL.Query().Get("id")]
	writeJSON(w, map[string]string{"id": c.ID, "tier": c.Tier})
}

func relatedSKUs(sku string) []string {
	return []string{"SKU-010", "SKU-011", "SKU-012"}
}

// demo bug 2: index out of range. The code expects four related items but gets three.
func (g *gw) recommendations(w http.ResponseWriter, r *http.Request) {
	related := relatedSKUs(r.URL.Query().Get("sku"))
	writeJSON(w, map[string]string{"featured": related[0], "fourth": related[3]})
}

// demo bug 3: integer divide by zero when qty is 0.
func (g *gw) discount(w http.ResponseWriter, r *http.Request) {
	total, _ := strconv.Atoi(r.URL.Query().Get("total"))
	qty, _ := strconv.Atoi(r.URL.Query().Get("qty"))
	perUnit := total / qty
	writeJSON(w, map[string]int{"per_unit": perUnit})
}

// demo bug 4: the error from a bad request body is not handled as a typed error, and the
// qty field is declared as an int so a string value fails to decode.
type checkoutItem struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

func decodeItems(r *http.Request) ([]checkoutItem, error) {
	var body struct {
		Items []checkoutItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err // *json.UnmarshalTypeError for {"qty":"two"}
	}
	return body.Items, nil
}

// Typed errors: Error Tracking groups on the Go type, so each of these is its own issue.
type InsufficientStockError struct {
	SKU        string
	Want, Have int
}

func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf("insufficient stock for %s: want %d, have %d", e.SKU, e.Want, e.Have)
}

type UnknownSKUError struct{ SKU string }

func (e *UnknownSKUError) Error() string { return "unknown sku " + e.SKU }

type PaymentFailedError struct {
	Status int
	Body   string
}

func (e *PaymentFailedError) Error() string {
	return fmt.Sprintf("payment failed with status %d: %s", e.Status, e.Body)
}
