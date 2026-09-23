// Command cf2otel polls the Cloudflare API and exports Access, HTTP, AI Gateway
// and other log and analytics surfaces as OpenTelemetry logs, metrics and traces.
//
// This is the pre-wave-1 scaffold: it only reports its build identity. The
// collector framework and exporters land in wave 1.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/rknightion/cf2otel/internal/collector"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("cf2otel %s (commit %s, built %s, %s)\n", version, commit, buildDate, runtime.Version())
		return
	}
	registerCollectors(collector.Deps{Registry: collector.NewRegistry()})

	fmt.Fprintln(os.Stderr, "cf2otel: scaffold build, no collectors are wired yet")
	os.Exit(2)
}
