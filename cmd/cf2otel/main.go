// Command cf2otel polls Cloudflare read APIs and exports OTLP signals.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	otellog "go.opentelemetry.io/otel/log"
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
	if err := validateDatasets(opts.Datasets); err != nil {
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
	var dryEmitter *dryRunEmitter
	if opts.DryRun {
		dryEmitter = &dryRunEmitter{writer: os.Stdout, counts: make(map[string]dryRunCounts)}
		emitter = dryEmitter
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
	var checkpoints collector.CheckpointStore = store
	if dryEmitter != nil {
		checkpoints = &dryRunCheckpointStore{base: store, values: make(map[string]time.Time)}
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
	deps := collector.Deps{Config: cfg, Emitter: emitter, API: api, Identity: idx, Apps: catalog, SelfObs: selfobs.NewCollector(stats), Checkpoints: checkpoints, Registry: registry}
	registerCollectors(deps)
	for _, entry := range registry.Entries() {
		stats.Expect(entry.Collector.Name())
		if _, ok := entry.Collector.(collector.WindowCollector); ok {
			configured := cfg.Collector(entry.Collector.Name()).MaxWindow
			effective := collector.EffectiveCommitWindow(entry)
			if effective > 0 && configured > effective {
				slog.Info("collector commit window capped", "collector", entry.Collector.Name(), "configured_max_window", configured, "effective_commit_window", effective)
			}
		}
		if entry.Interval < cli.MinimumInterval {
			return fmt.Errorf("collector %s interval %s is below minimum %s", entry.Collector.Name(), entry.Interval, cli.MinimumInterval)
		}
	}
	scheduler := collector.NewScheduler(registry, emitter, checkpoints)
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

func validateDatasets(datasets []string) error {
	if len(datasets) == 0 {
		return nil
	}
	configured := config.Default().Collectors
	valid := make([]string, 0, len(configured))
	known := make(map[string]struct{}, len(configured))
	for name := range configured {
		valid = append(valid, name)
		known[name] = struct{}{}
	}
	sort.Strings(valid)
	var unknown []string
	for _, name := range datasets {
		if _, ok := known[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown dataset(s) %s; valid names: %s", strings.Join(unknown, ", "), strings.Join(valid, ", "))
	}
	return nil
}

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
	dryEmitter, dryRun := s.Emitter.(*dryRunEmitter)
	for _, entry := range s.Registry.Entries() {
		if !selected(entry.Collector.Name(), o.Datasets) {
			continue
		}
		if dryRun {
			dryEmitter.beginCollector(entry.Collector.Name())
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
		if dryRun {
			dryEmitter.finishCollector(entry.Collector.Name())
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

type dryRunCounts struct {
	records int
	metrics int
}

// dryRunEmitter counts what collectors would have exported without creating OTLP providers.
type dryRunEmitter struct {
	mu      sync.Mutex
	writer  io.Writer
	current string
	counts  map[string]dryRunCounts
}

func (e *dryRunEmitter) beginCollector(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.current = name
	e.counts[name] = dryRunCounts{}
}

func (e *dryRunEmitter) finishCollector(name string) {
	e.mu.Lock()
	counts := e.counts[name]
	e.current = ""
	e.mu.Unlock()
	_, _ = fmt.Fprintf(e.writer, "dry-run %s: records=%d metrics=%d\n", name, counts.records, counts.metrics)
}

func (e *dryRunEmitter) countRecord() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current != "" {
		counts := e.counts[e.current]
		counts.records++
		e.counts[e.current] = counts
	}
}

func (e *dryRunEmitter) countMetric() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current != "" {
		counts := e.counts[e.current]
		counts.metrics++
		e.counts[e.current] = counts
	}
}

func (e *dryRunEmitter) Gauge(context.Context, string, float64, ...telemetry.Attr) error {
	e.countMetric()
	return nil
}
func (e *dryRunEmitter) Counter(context.Context, string, float64, ...telemetry.Attr) error {
	e.countMetric()
	return nil
}
func (e *dryRunEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error {
	e.countMetric()
	return nil
}
func (e *dryRunEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	e.countRecord()
	return nil
}
func (e *dryRunEmitter) Span(context.Context, telemetry.SpanSpec) error {
	e.countRecord()
	return nil
}

// dryRunCheckpointStore lets a dry-run collector advance its in-memory cursor
// while preserving the checkpoint file that seeds its collection window.
type dryRunCheckpointStore struct {
	mu     sync.RWMutex
	base   collector.CheckpointStore
	values map[string]time.Time
}

func (s *dryRunCheckpointStore) Get(name string) (time.Time, bool) {
	s.mu.RLock()
	value, ok := s.values[name]
	s.mu.RUnlock()
	if ok {
		return value, true
	}
	return s.base.Get(name)
}

func (s *dryRunCheckpointStore) Set(name string, value time.Time) error {
	s.mu.Lock()
	s.values[name] = value
	s.mu.Unlock()
	return nil
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
