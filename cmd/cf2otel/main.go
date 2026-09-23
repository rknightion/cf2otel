// Command cf2otel polls Cloudflare read APIs and exports OTLP signals.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/cli"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/inventory"
	"github.com/rknightion/cf2otel/internal/collectors/selfobs"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/health"
	"github.com/rknightion/cf2otel/internal/identity"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("cf2otel stopped", "error", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	opts, err := cli.Parse(args)
	if err != nil {
		return err
	}
	if opts.Version {
		fmt.Printf("cf2otel %s (commit %s, built %s, %s)\n", version, commit, buildDate, runtime.Version())
		return nil
	}
	cfg, err := config.Load(opts.Config)
	if err != nil {
		return err
	}
	if opts.Healthcheck {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return health.ProbeListen(ctx, cfg.Health.Listen)
	}
	if opts.Config != "" {
		if advisory := cli.ConfigPermissionAdvisory(opts.Config); advisory != "" {
			slog.Warn(advisory)
		}
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if opts.Validate {
		fmt.Println("config valid")
		return nil
	}
	if opts.PrintEffectiveConfig {
		b, err := cfg.RedactedJSON()
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var providers *telemetry.Providers
	var emitter telemetry.Emitter
	if opts.DryRun {
		emitter = telemetry.NewNoopEmitter()
	} else {
		providers, err = telemetry.NewProviders(ctx, telemetry.ProviderOptions{Endpoint: cfg.OTLP.Endpoint, Protocol: cfg.OTLP.Protocol, InstanceID: cfg.OTLP.GrafanaCloud.InstanceID, Token: cfg.OTLP.GrafanaCloud.Token.Value(), ServiceVersion: version, InstanceUUID: hostname(), Headers: cfg.OTLP.Headers})
		if err != nil {
			return err
		}
		emitter = providers.Emitter
		defer func() {
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if e := providers.Shutdown(shutdown); e != nil {
				slog.Error("OTLP shutdown failed", "error", e)
			}
		}()
	}
	stats := selfobs.New(emitter, version, commit)
	if providers != nil {
		providers.SetExportObserver(func(ctx context.Context, signal string, exportErr error) {
			if err := stats.Export(ctx, signal, exportErr); err != nil {
				slog.Error("self-observability export outcome emission failed")
			}
		})
	}
	observer := func(method, route string, status int, duration time.Duration, retry bool) {
		_ = route // No request path, ID, or query can enter a metric dimension.
		ended := time.Now()
		attrs := []telemetry.Attr{{Key: semconv.AttrStatusClass, Value: fmt.Sprintf("%dxx", status/100)}}
		_ = emitter.Counter(ctx, semconv.MetricAPIRequests, 1, attrs...)
		_ = emitter.Histogram(ctx, semconv.MetricAPIDuration, duration.Seconds(), attrs...)
		_ = emitter.Span(ctx, telemetry.SpanSpec{Name: semconv.SpanAPIRequest, Start: ended.Add(-duration), End: ended, Kind: trace.SpanKindClient, Attrs: append(attrs, telemetry.Attr{Key: semconv.AttrAPIMethod, Value: method})})
		if retry {
			_ = emitter.Counter(ctx, semconv.MetricAPIRetries, 1, attrs...)
		}
	}
	api := cfapi.NewObserved(cfg.Cloudflare, observer)
	if opts.Explore != "" {
		return explore(ctx, api, cfg, opts.Explore)
	}
	statePath := filepath.Join(cfg.State.Dir, "checkpoints.json")
	if opts.ResetState {
		if err := archiveCheckpoint(statePath); err != nil {
			return err
		}
	}
	store, err := collector.NewFileStore(statePath)
	if err != nil {
		return err
	}
	catalog := inventory.NewCatalog()
	var idx identity.Index
	if cfg.Identity.Enabled {
		memoryIndex, err := identity.NewWithRetention(cfg.Identity.MatchWindow, 23*time.Hour, cfg.Identity.MaxCandidates)
		if err != nil {
			return err
		}
		idx = memoryIndex
		stats.SetIdentityStats(memoryIndex.Stats)
		go func(index *identity.MemoryIndex) {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case now := <-ticker.C:
					index.Prune(now)
				}
			}
		}(memoryIndex)
	}
	registry := collector.NewRegistry()
	deps := collector.Deps{Config: cfg, Emitter: emitter, API: api, Identity: idx, Apps: catalog, SelfObs: selfobs.NewCollector(stats), Checkpoints: store, Registry: registry}
	registerCollectors(deps)
	for _, entry := range registry.Entries() {
		stats.Expect(entry.Collector.Name())
		if entry.Interval < cli.MinimumInterval {
			return fmt.Errorf("collector %s interval %s is below minimum %s", entry.Collector.Name(), entry.Interval, cli.MinimumInterval)
		}
	}
	scheduler := collector.NewScheduler(registry, emitter, store)
	if providers != nil {
		scheduler.Flusher = providers
	}
	scheduler.OnPoll = func(ctx context.Context, name string, duration time.Duration, err error, at time.Time) {
		if e := stats.Poll(ctx, name, duration, err, at); e != nil {
			slog.Error("self-observability emission failed", "error", e)
		}
	}
	scheduler.OnCheckpoint = stats.Checkpoint
	if opts.Once || !opts.Since.IsZero() || !opts.Before.IsZero() {
		return runOnce(ctx, scheduler, opts)
	}
	go func() {
		if e := health.Serve(ctx, cfg); e != nil {
			slog.Error("health server failed", "error", e)
			stop()
		}
	}()
	scheduler.Run(ctx)
	return nil
}
func hostname() string { h, _ := os.Hostname(); return h }
func selected(name string, names []string) bool {
	if len(names) == 0 {
		return true
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
func runOnce(ctx context.Context, s *collector.Scheduler, o cli.Options) error {
	var errs []error
	for _, entry := range s.Registry.Entries() {
		if !selected(entry.Collector.Name(), o.Datasets) {
			continue
		}
		start := time.Now()
		var err error
		if c, ok := entry.Collector.(collector.WindowCollector); ok && (!o.Since.IsZero() || !o.Before.IsZero()) {
			if o.Since.IsZero() || o.Before.IsZero() {
				return errors.New("-since and -before must be supplied together")
			}
			err = s.CollectRange(ctx, c, o.Since, o.Before)
		} else {
			err = s.RunOnce(ctx, entry)
		}
		if s.OnPoll != nil {
			s.OnPoll(ctx, entry.Collector.Name(), time.Since(start), err, time.Now())
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Collector.Name(), err))
		}
	}
	return errors.Join(errs...)
}
func archiveCheckpoint(path string) error {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.Rename(path, path+".bak-"+time.Now().UTC().Format("20060102T150405Z"))
}
func explore(ctx context.Context, api cfapi.Client, cfg *config.Config, kind string) error {
	var path string
	switch strings.ToLower(kind) {
	case "access":
		path = "/accounts/" + cfg.Cloudflare.AccountID + "/access/apps"
	case "ai_gateway":
		path = "/accounts/" + cfg.Cloudflare.AccountID + "/ai-gateway/gateways"
	case "http":
		_, err := api.Zones(ctx)
		if err != nil {
			return err
		}
		fmt.Println("http: zone discovery succeeded")
		return nil
	default:
		return fmt.Errorf("unknown explore dataset %q", kind)
	}
	var rows []map[string]any
	if err := api.Get(ctx, path, nil, &rows); err != nil {
		if strings.Contains(err.Error(), "cloudflare HTTP 403") {
			return errors.New(cli.ExploreSummary(kind, 403, path))
		}
		return err
	}
	fmt.Printf("%s: %d records visible\n", kind, len(rows))
	return nil
}
