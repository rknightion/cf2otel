package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
)

func main() {
	spec := flag.String("spec", "spec/cloudflare/contract.json", "committed sanitized contract")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	c, err := loadContract(*spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid drift contract")
		os.Exit(2)
	}
	token := os.Getenv("CLOUDFLARE_API_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "CLOUDFLARE_API_TOKEN is required")
		os.Exit(2)
	}
	// Leave thirty seconds of the ten-minute job for startup and reporting.
	ctx, cancel := context.WithTimeout(context.Background(), 570*time.Second)
	defer cancel()
	cfg := config.Default().Cloudflare
	cfg.APIToken, cfg.Timeout, cfg.MaxResponseBytes = config.Secret(token), 10*time.Second, 2<<20
	if err := cfapi.ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
		fmt.Fprintln(os.Stderr, "invalid probe rate configuration")
		os.Exit(2)
	}
	api := cfapi.New(cfg)
	for _, report := range documentedRESTReports(c) {
		fmt.Println(report)
	}
	diffs := probeBudgeted(ctx, api, c, cfg.RateLimit)
	for _, diff := range diffs {
		fmt.Fprintln(os.Stderr, diff)
	}
	if len(diffs) > 0 {
		fmt.Fprintf(os.Stderr, "Cloudflare API drift: %d difference(s)\n", len(diffs))
		os.Exit(1)
	}
	fmt.Println("Cloudflare API contract matched")
}
