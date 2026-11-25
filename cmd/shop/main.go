// Command shop runs one of four roles, chosen by SHOP_ROLE:
// gateway, inventory, payment or loadgen.
package main

import (
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	"os"
	"os/signal"
	"syscall"

	"github.com/datadog-asean-se/storedog-go-apm-example/internal/gateway"
	"github.com/datadog-asean-se/storedog-go-apm-example/internal/inventory"
	"github.com/datadog-asean-se/storedog-go-apm-example/internal/loadgen"
	"github.com/datadog-asean-se/storedog-go-apm-example/internal/payment"
	"github.com/datadog-asean-se/storedog-go-apm-example/internal/telemetry"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log := telemetry.Logger()
	log.Info("starting", "role", os.Getenv("SHOP_ROLE"), "build_version", version)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; tracer.Stop(); os.Exit(0) }() // flush buffered spans on SIGTERM

	addr := ":" + env("PORT", "8080")
	var err error
	switch role := os.Getenv("SHOP_ROLE"); role {
	case "gateway":
		err = gateway.Run(addr, log)
	case "inventory":
		err = inventory.Run(addr, log)
	case "payment":
		err = payment.Run(addr, log)
	case "loadgen":
		loadgen.Run(log)
	default:
		log.Error("set SHOP_ROLE to gateway|inventory|payment|loadgen", "got", role)
		os.Exit(2)
	}
	if err != nil {
		log.Error("exited", "err", err)
		tracer.Stop()
		os.Exit(1)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
