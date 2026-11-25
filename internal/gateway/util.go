package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
