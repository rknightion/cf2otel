package logpush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const (
	logpushCollectorName = "logpush.health"
	logpushDataset       = "logpushHealthAdaptiveGroups"
	logpushQueryLimit    = 10000
	logpushBucket        = 5 * time.Minute
)

var logpushFields = []string{"sum.uploads", "sum.records", "dimensions.datetimeFiveMinutes"}

type logpushSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type healthMetrics struct {
	cfg *config.Config
	api cfapi.Client
}

func NewHealthMetrics(cfg *config.Config, api cfapi.Client) *healthMetrics {
	return &healthMetrics{cfg: cfg, api: api}
}

func (*healthMetrics) Name() string                   { return logpushCollectorName }
func (*healthMetrics) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*healthMetrics) Lag() time.Duration             { return 10 * time.Minute }

func (c *healthMetrics) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid Logpush health window")
	}
	windowTo := to.UTC().Truncate(logpushBucket)
	if !windowTo.After(from) {
		return from, errors.New("logpush health window contains no complete five-minute bucket")
	}
	windowFrom := from.UTC().Truncate(logpushBucket)
	if windowFrom.Before(from) {
		windowFrom = windowFrom.Add(logpushBucket)
	}
	if !windowFrom.Before(windowTo) {
		return from, errors.New("window contains no complete five-minute bucket")
	}
	if c.cfg == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("logpush health requires a configured Cloudflare account")
	}
	if c.api == nil {
		return from, errors.New("logpush health requires a Cloudflare client")
	}
	seriesCap := c.cfg.Platform.MaxMetricSeriesPerWindow
	if seriesCap <= 0 {
		return from, errors.New("logpush health has an invalid platform metric series limit")
	}
	reader, ok := c.api.(logpushSettingsReader)
	if !ok {
		return from, errors.New("cloudflare client does not expose Logpush health dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, logpushDataset)
	if err != nil {
		return from, fmt.Errorf("read Logpush health dataset settings: %w", err)
	}
	if !settings.Enabled {
		return from, errors.New("logpush health dataset is disabled")
	}
	if settings.MaxDuration <= 0 || settings.NotOlderThan <= 0 {
		return from, errors.New("logpush health dataset is missing its duration or retention limit")
	}
	if settings.MaxPageSize <= 0 {
		return from, errors.New("logpush health dataset is missing its page-size limit")
	}
	if settings.MaxNumberOfFields < len(logpushFields) {
		return from, errors.New("logpush health dataset field limit is below the required field count")
	}
	for _, field := range logpushFields {
		if !logpushHasAvailableField(settings.AvailableFields, field) {
			return from, fmt.Errorf("logpush health dataset is missing required field %s", field)
		}
	}
	limit := logpushQueryLimit
	if settings.MaxPageSize < limit {
		limit = settings.MaxPageSize
	}
	request := cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      logpushDataset,
		WantedFields: append([]string(nil), logpushFields...),
		Limit:        limit,
	}
	rows, err := c.queryWindow(ctx, request, windowFrom, windowTo, limit)
	if err != nil {
		return from, fmt.Errorf("query Logpush health dataset: %w", err)
	}

	var uploads, records float64
	hasCompleteBucket := false
	for _, row := range rows {
		bucket, err := logpushRowBucket(row)
		if err != nil {
			return from, err
		}
		if !logpushCompleteBucket(bucket, from, windowTo) {
			continue
		}
		sums, ok := row["sum"].(map[string]any)
		if !ok {
			return from, errors.New("logpush health row is missing sum fields")
		}
		uploadCount, ok := logpushCount(sums["uploads"])
		if !ok {
			return from, errors.New("logpush health row has an invalid upload sum")
		}
		recordCount, ok := logpushCount(sums["records"])
		if !ok {
			return from, errors.New("logpush health row has an invalid record sum")
		}
		uploads += uploadCount
		records += recordCount
		hasCompleteBucket = true
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}
	if !hasCompleteBucket {
		return windowTo, nil
	}

	series := []telemetry.BufferedMetric{
		{Kind: "counter", Name: semconv.MetricLogpushUploads, Value: uploads},
		{Kind: "counter", Name: semconv.MetricLogpushRecords, Value: records},
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Name < series[j].Name })
	dropped := 0
	if len(series) > seriesCap {
		dropped = len(series) - seriesCap
		series = series[:seriesCap]
		slog.WarnContext(ctx, "platform metric series dropped", "collector", logpushCollectorName, "dropped_series", dropped)
	}
	for _, metric := range series {
		if err := out.Counter(ctx, metric.Name, metric.Value); err != nil {
			return from, err
		}
	}
	return windowTo, nil
}

func (c *healthMetrics) queryWindow(ctx context.Context, request cfapi.GraphQLRequest, from, to time.Time, limit int) ([]map[string]any, error) {
	request.From = from
	request.To = to
	request.Limit = limit
	var rows []map[string]any
	err := c.api.Query(ctx, request, &rows)
	saturated := logpushIsSaturation(err) || (err == nil && len(rows) >= limit)
	if !saturated {
		if err != nil {
			return nil, err
		}
		return rows, nil
	}
	if to.Sub(from) <= logpushBucket {
		if err == nil {
			err = fmt.Errorf("GraphQL dataset %s reached requested limit %d", logpushDataset, limit)
		}
		return nil, fmt.Errorf("logpush health query still saturates a five-minute bucket: %w", err)
	}
	mid := from.Add(to.Sub(from) / 2).Truncate(logpushBucket)
	if !from.Before(mid) || !mid.Before(to) {
		return nil, fmt.Errorf("cannot split saturated Logpush health window %s..%s", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
	left, err := c.queryWindow(ctx, request, from, mid, limit)
	if err != nil {
		return nil, err
	}
	right, err := c.queryWindow(ctx, request, mid, to, limit)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func logpushIsSaturation(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "GraphQL dataset "+logpushDataset+" window ") && strings.Contains(message, " saturated limit ")
}

func logpushHasAvailableField(available []string, wanted string) bool {
	wanted = strings.ToLower(wanted)
	flatWanted := strings.ReplaceAll(wanted, ".", "_")
	for _, field := range available {
		field = strings.ToLower(strings.TrimSpace(field))
		if field == wanted || field == flatWanted {
			return true
		}
	}
	return false
}

func logpushRowBucket(row map[string]any) (time.Time, error) {
	dimensions, ok := row["dimensions"].(map[string]any)
	if !ok {
		return time.Time{}, errors.New("logpush health row is missing dimensions")
	}
	raw, ok := dimensions["datetimeFiveMinutes"].(string)
	if !ok {
		return time.Time{}, errors.New("logpush health row is missing its five-minute timestamp")
	}
	bucket, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("logpush health row has an invalid five-minute timestamp")
	}
	bucket = bucket.UTC()
	if !bucket.Equal(bucket.Truncate(logpushBucket)) {
		return time.Time{}, errors.New("logpush health row timestamp is not a five-minute bucket")
	}
	return bucket, nil
}

func logpushCompleteBucket(bucket, from, to time.Time) bool {
	return !bucket.Before(from) && !bucket.Add(logpushBucket).After(to)
}

func logpushCount(value any) (float64, bool) {
	var count float64
	switch n := value.(type) {
	case float64:
		count = n
	case float32:
		count = float64(n)
	case int:
		count = float64(n)
	case int32:
		count = float64(n)
	case int64:
		count = float64(n)
	case uint:
		count = float64(n)
	case uint32:
		count = float64(n)
	case uint64:
		count = float64(n)
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, false
		}
		count = parsed
	case string:
		parsed, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		count = parsed
	default:
		return 0, false
	}
	return count, count >= 0 && !math.IsNaN(count) && !math.IsInf(count, 0) && math.Trunc(count) == count
}

var _ collector.WindowCollector = (*healthMetrics)(nil)
