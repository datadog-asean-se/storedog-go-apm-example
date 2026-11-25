package gateway

import (
	"bytes"
	"io"
	"runtime/debug"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
func stack() string                  { return string(debug.Stack()) }
