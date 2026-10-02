package workersai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const dataset = "aiInferenceAdaptiveGroups"
const bucketWidth = 5 * time.Minute
const timeField = "dimensions.datetimeFiveMinutes"

type metricsCollector struct {
	cfg *config.Config
	api cfapi.Client
}

func (*metricsCollector) Name() string                   { return semconv.CollectorNameWorkersAIMetrics }
func (*metricsCollector) DefaultInterval() time.Duration { return bucketWidth }
func (*metricsCollector) Lag() time.Duration             { return 10 * time.Minute }

// LagAt aligns the scheduler upper bound, avoiding an incomplete startup bucket.
// The holdback is operational policy, not an ingestion-latency guarantee.
func (c *metricsCollector) LagAt(now time.Time) time.Duration {
	return now.Sub(now.UTC().Add(-c.Lag()).Truncate(bucketWidth))
}

type settingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}
type fieldSpec struct {
	field, metric string
	divisor       float64
}

var fields = []fieldSpec{
	{"count", semconv.MetricWorkersAIInferences, 1},
	{"sum.totalInputTokens", semconv.MetricWorkersAIInputTokens, 1},
	{"sum.totalOutputTokens", semconv.MetricWorkersAIOutputTokens, 1},
	{"sum.totalInferenceTimeMs", semconv.MetricWorkersAIInferenceTime, 1000},
}

func (c *metricsCollector) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid Workers AI window")
	}
	if c.cfg == nil || c.cfg.Cloudflare.AccountID == "" || c.api == nil || out == nil {
		return from, errors.New("workers AI requires account, client and emitter")
	}
	cap := c.cfg.Platform.MaxMetricSeriesPerWindow
	if cap <= 0 {
		return from, errors.New("platform.max_metric_series_per_window must be positive")
	}
	reader, ok := c.api.(settingsReader)
	if !ok {
		return from, errors.New("workers AI requires dataset settings")
	}
	batch, ok := c.api.(cfapi.GraphQLBatchQuerier)
	if !ok {
		return from, errors.New("workers AI requires raw GraphQL batch queries")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, dataset)
	if err != nil {
		return from, err
	}
	if !settings.Enabled || settings.MaxNumberOfFields < 2 || settings.MaxPageSize <= 0 || settings.MaxDuration <= 0 || settings.NotOlderThan <= 0 {
		return from, errors.New("invalid or disabled Workers AI dataset settings")
	}
	if !available(settings.AvailableFields, "count") || !available(settings.AvailableFields, timeField) {
		return from, errors.New("workers AI missing required count or bucket field")
	}
	selected := []fieldSpec{fields[0]}
	wanted := []string{"count", timeField}
	for _, f := range fields[1:] {
		if available(settings.AvailableFields, f.field) && len(wanted) < settings.MaxNumberOfFields {
			selected = append(selected, f)
			wanted = append(wanted, f.field)
		}
	}
	start := from.UTC().Truncate(bucketWidth)
	if start.Before(from) {
		start = start.Add(bucketWidth)
	}
	end := to.UTC().Truncate(bucketWidth)
	if !start.Before(end) {
		return from, errors.New("workers AI window contains no complete bucket")
	}
	duration := time.Duration(settings.MaxDuration) * time.Second / bucketWidth * bucketWidth
	if duration < bucketWidth {
		return from, errors.New("workers AI duration limit cannot cover a complete bucket")
	}
	request := cfapi.GraphQLRequest{Scope: cfapi.AccountScope, ScopeID: c.cfg.Cloudflare.AccountID, Dataset: dataset, WantedFields: wanted, Limit: min(settings.MaxPageSize, 10000)}
	totals := make([]float64, len(selected))
	seen := make([]bool, len(selected))
	for a := start; a.Before(end); {
		b := a.Add(duration)
		if b.After(end) {
			b = end
		}
		rows, err := collector.Bisect(a, b, bucketWidth, bucketWidth, func(a, b time.Time) ([]map[string]any, bool, error) {
			leaf := request
			leaf.From = a
			leaf.To = b
			result, err := batch.QueryBatch(ctx, []cfapi.GraphQLBatchSelection{{Alias: "ai", Request: leaf}})
			if err != nil {
				sat, ok := cfapi.AsSaturation(err)
				return nil, ok && sat.Dataset == dataset, err
			}
			raw := bytes.TrimSpace(result["ai"])
			if len(raw) == 0 || raw[0] != '[' {
				return nil, false, errors.New("workers AI dataset must be a nonnull row array")
			}
			var rows []map[string]any
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&rows); err != nil {
				return nil, false, err
			}
			if len(rows) >= leaf.Limit {
				return nil, true, &cfapi.SaturationError{Dataset: dataset, From: a, To: b, Limit: leaf.Limit}
			}
			for _, row := range rows {
				dims, _ := row["dimensions"].(map[string]any)
				stamp, _ := dims["datetimeFiveMinutes"].(string)
				t, err := time.Parse(time.RFC3339Nano, stamp)
				if err != nil || !t.Equal(t.UTC().Truncate(bucketWidth)) || t.Before(a) || !t.Before(b) || t.Add(bucketWidth).After(b) {
					return nil, false, errors.New("workers AI returned invalid, incomplete or out-of-window bucket")
				}
			}
			return rows, false, nil
		})
		if err != nil {
			return from, err
		}
		for _, row := range rows {
			for i, f := range selected {
				raw := row[f.field]
				if group, key, ok := strings.Cut(f.field, "."); ok {
					values, _ := row[group].(map[string]any)
					raw = values[key]
				}
				if raw == nil && i > 0 {
					continue
				}
				n, ok := raw.(json.Number)
				if !ok {
					return from, fmt.Errorf("workers AI invalid numeric field %s", f.field)
				}
				value, err := n.Float64()
				if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
					return from, fmt.Errorf("workers AI nonfinite or negative field %s", f.field)
				}
				totals[i] += value / f.divisor
				seen[i] = true
				if math.IsInf(totals[i], 0) {
					return from, errors.New("workers AI counter overflow")
				}
			}
		}
		a = b
	}
	type point struct {
		name  string
		value float64
	}
	var points []point
	for i, f := range selected {
		if seen[i] {
			points = append(points, point{f.metric, totals[i]})
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].name < points[j].name })
	if len(points) > cap {
		slog.Warn("platform metric series dropped", "collector", c.Name(), "dropped", len(points)-cap)
		points = points[:cap]
	}
	for _, p := range points {
		if err := out.Counter(ctx, p.name, p.value); err != nil {
			return from, err
		}
	}
	return end, nil
}

func available(advertised []string, field string) bool {
	for _, a := range advertised {
		if a == field || a == strings.ReplaceAll(field, ".", "_") {
			return true
		}
	}
	return false
}

var _ collector.WindowCollector = (*metricsCollector)(nil)
var _ collector.WindowLagAt = (*metricsCollector)(nil)
