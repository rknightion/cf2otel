package kv

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
	kvQueryLimit         = 10000
	kvBucketDuration     = 5 * time.Minute
	kvMinimumQueryWindow = 5 * time.Minute
)

type metricKind uint8

const (
	counterMetric metricKind = iota
	gaugeMetric
)

type metricSpec struct {
	name  string
	field string
	kind  metricKind
}

type datasetSpec struct {
	collector string
	dataset   string
	metrics   []metricSpec
}

var datasets = []datasetSpec{
	{
		collector: "kv.operations",
		dataset:   "kvOperationsAdaptiveGroups",
		metrics:   []metricSpec{{name: semconv.MetricKVRequests, field: "sum.requests", kind: counterMetric}},
	},
	{
		collector: "kv.storage",
		dataset:   "kvStorageAdaptiveGroups",
		metrics: []metricSpec{
			{name: semconv.MetricKVStorageBytes, field: "max.byteCount", kind: gaugeMetric},
			{name: semconv.MetricKVStorageKeys, field: "max.keyCount", kind: gaugeMetric},
		},
	},
}

type settingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type groupsCollector struct {
	cfg  *config.Config
	api  cfapi.Client
	spec datasetSpec
}

type metricValue struct {
	name  string
	value float64
	kind  metricKind
}

func newGroupsCollector(cfg *config.Config, api cfapi.Client, spec datasetSpec) *groupsCollector {
	return &groupsCollector{cfg: cfg, api: api, spec: spec}
}

func (c *groupsCollector) Name() string                 { return c.spec.collector }
func (*groupsCollector) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*groupsCollector) Lag() time.Duration             { return 10 * time.Minute }

func (c *groupsCollector) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid KV Groups window")
	}
	if c.cfg == nil || c.api == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("KV Groups collector requires a configured Cloudflare account and API")
	}
	reader, ok := c.api.(settingsReader)
	if !ok {
		return from, errors.New("cloudflare client does not expose KV Groups dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, c.spec.dataset)
	if err != nil {
		return from, fmt.Errorf("read KV Groups settings for %s: %w", c.spec.dataset, err)
	}
	if !settings.Enabled {
		return from, fmt.Errorf("KV Groups dataset %s is disabled", c.spec.dataset)
	}
	if settings.MaxNumberOfFields <= 0 {
		return from, fmt.Errorf("KV Groups dataset %s has an invalid field limit", c.spec.dataset)
	}
	if settings.MaxPageSize <= 0 {
		return from, fmt.Errorf("KV Groups dataset %s has an invalid page-size limit", c.spec.dataset)
	}
	wanted, err := c.requiredFields(settings)
	if err != nil {
		return from, err
	}
	completeFrom := ceilBucket(from)
	completeTo := floorBucket(to)
	if completeTo.Before(from) {
		completeTo = from
	}
	if !completeFrom.Before(completeTo) {
		return from, errors.New("window contains no complete five-minute bucket")
	}
	limit := min(kvQueryLimit, settings.MaxPageSize)
	request := cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      c.spec.dataset,
		WantedFields: wanted,
		From:         completeFrom,
		To:           completeTo,
		Limit:        limit,
	}
	rows, err := c.queryRows(ctx, request)
	if err != nil {
		return from, fmt.Errorf("query KV Groups dataset %s: %w", c.spec.dataset, err)
	}
	values, err := c.aggregate(rows, completeFrom, completeTo)
	if err != nil {
		return from, err
	}
	values, err = c.applySeriesCap(ctx, values)
	if err != nil {
		return from, err
	}
	for _, metric := range values {
		var err error
		if metric.kind == gaugeMetric {
			err = out.Gauge(ctx, metric.name, metric.value)
		} else {
			err = out.Counter(ctx, metric.name, metric.value)
		}
		if err != nil {
			return from, err
		}
	}
	return completeTo, nil
}

func (c *groupsCollector) requiredFields(settings cfapi.DatasetSettings) ([]string, error) {
	fields := make([]string, 0, len(c.spec.metrics)+1)
	for _, metric := range c.spec.metrics {
		fields = append(fields, metric.field)
	}
	fields = append(fields, "dimensions.datetimeFiveMinutes")
	for _, field := range fields {
		if !hasAvailableField(settings.AvailableFields, field) {
			return nil, fmt.Errorf("KV Groups dataset %s is missing required field %s", c.spec.dataset, field)
		}
	}
	if len(fields) > settings.MaxNumberOfFields {
		return nil, fmt.Errorf("KV Groups dataset %s requires %d fields, limit %d", c.spec.dataset, len(fields), settings.MaxNumberOfFields)
	}
	// The frozen KV contract has no resource metric attribute. Do not select an
	// optional resource dimension; all rows contribute to one account series.
	return fields, nil
}

func hasAvailableField(available []string, wanted string) bool {
	for _, field := range available {
		if strings.EqualFold(field, wanted) {
			return true
		}
		if prefix, suffix, ok := strings.Cut(wanted, "."); ok && strings.EqualFold(field, prefix+"_"+suffix) {
			return true
		}
	}
	return false
}

func (c *groupsCollector) queryRows(ctx context.Context, request cfapi.GraphQLRequest) ([]map[string]any, error) {
	var rows []map[string]any
	err := c.api.Query(ctx, request, &rows)
	saturated := isSaturationError(err, request.Dataset)
	if err == nil && len(rows) >= request.Limit {
		saturated = true
		err = fmt.Errorf("result reached requested limit %d", request.Limit)
	}
	if err != nil && !saturated {
		return nil, err
	}
	if !saturated {
		return rows, nil
	}
	if request.To.Sub(request.From) <= kvMinimumQueryWindow {
		return nil, fmt.Errorf("KV Groups query still saturates an irreducible five-minute window: %w", err)
	}
	mid, ok := bucketSplit(request.From, request.To)
	if !ok {
		return nil, fmt.Errorf("cannot split saturated KV Groups window %s..%s: %w", request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), err)
	}
	leftRequest := request
	leftRequest.To = mid
	left, err := c.queryRows(ctx, leftRequest)
	if err != nil {
		return nil, err
	}
	rightRequest := request
	rightRequest.From = mid
	right, err := c.queryRows(ctx, rightRequest)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func isSaturationError(err error, dataset string) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "GraphQL dataset "+dataset+" window ") && strings.Contains(message, " saturated limit ")
}

func bucketSplit(from, to time.Time) (time.Time, bool) {
	mid := from.Add(to.Sub(from) / 2).Truncate(5 * time.Minute)
	if !mid.After(from) {
		mid = from.Truncate(5 * time.Minute).Add(5 * time.Minute)
	}
	if !mid.Before(to) {
		mid = to.Truncate(5 * time.Minute).Add(-5 * time.Minute)
	}
	return mid, from.Before(mid) && mid.Before(to)
}

func (c *groupsCollector) aggregate(rows []map[string]any, from, to time.Time) ([]metricValue, error) {
	counters := make(map[string]float64, len(c.spec.metrics))
	counterSeen := make(map[string]bool, len(c.spec.metrics))
	gauges := make(map[string]float64, len(c.spec.metrics))
	gaugeSeen := make(map[string]bool, len(c.spec.metrics))
	var latestBucket time.Time

	for _, row := range rows {
		bucket, err := rowBucket(row)
		if err != nil {
			return nil, fmt.Errorf("KV Groups dataset %s returned an invalid five-minute bucket", c.spec.dataset)
		}
		if bucket.Before(from) || !bucket.Before(to) || bucket.Add(kvBucketDuration).After(to) {
			continue
		}
		rowValues := make(map[string]float64, len(c.spec.metrics))
		for _, metric := range c.spec.metrics {
			value, ok := numericField(row, metric.field)
			if !ok {
				return nil, fmt.Errorf("KV Groups dataset %s returned a row without valid %s", c.spec.dataset, metric.field)
			}
			rowValues[metric.name] = value
		}
		for _, metric := range c.spec.metrics {
			value := rowValues[metric.name]
			if metric.kind == counterMetric {
				counters[metric.name] += value
				counterSeen[metric.name] = true
				continue
			}
			if bucket.After(latestBucket) {
				latestBucket = bucket
				clear(gauges)
				clear(gaugeSeen)
			}
			if bucket.Equal(latestBucket) && (!gaugeSeen[metric.name] || value > gauges[metric.name]) {
				gauges[metric.name] = value
				gaugeSeen[metric.name] = true
			}
		}
	}

	values := make([]metricValue, 0, len(c.spec.metrics))
	for _, metric := range c.spec.metrics {
		if metric.kind == counterMetric && counterSeen[metric.name] {
			values = append(values, metricValue{name: metric.name, value: counters[metric.name], kind: counterMetric})
		}
		if metric.kind == gaugeMetric && gaugeSeen[metric.name] {
			values = append(values, metricValue{name: metric.name, value: gauges[metric.name], kind: gaugeMetric})
		}
	}
	return values, nil
}

func rowBucket(row map[string]any) (time.Time, error) {
	dimensions, ok := row["dimensions"].(map[string]any)
	if !ok {
		return time.Time{}, errors.New("missing dimensions")
	}
	value, ok := dimensions["datetimeFiveMinutes"]
	if !ok {
		return time.Time{}, errors.New("missing datetimeFiveMinutes")
	}
	switch value := value.(type) {
	case time.Time:
		return value.UTC(), nil
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, value)
		return parsed.UTC(), err
	default:
		return time.Time{}, errors.New("invalid datetimeFiveMinutes type")
	}
}

func floorBucket(value time.Time) time.Time {
	return value.UTC().Truncate(kvBucketDuration)
}

func ceilBucket(value time.Time) time.Time {
	floor := floorBucket(value)
	if floor.Equal(value.UTC()) {
		return floor
	}
	return floor.Add(kvBucketDuration)
}

func numericField(row map[string]any, field string) (float64, bool) {
	parts := strings.SplitN(field, ".", 2)
	var value any
	if len(parts) == 1 {
		value = row[field]
	} else {
		group, ok := row[parts[0]].(map[string]any)
		if !ok {
			return 0, false
		}
		value = group[parts[1]]
	}
	if value == nil {
		return 0, false
	}
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case float32:
		number = float64(value)
	case int:
		number = float64(value)
	case int32:
		number = float64(value)
	case int64:
		number = float64(value)
	case uint:
		number = float64(value)
	case uint32:
		number = float64(value)
	case uint64:
		number = float64(value)
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	return number, number >= 0 && !math.IsNaN(number) && !math.IsInf(number, 0)
}

// There are no KV resource metric attributes in the frozen contract, so
// 500 synthetic namespaces still collapse to at most two account series.
// The default 500-series drop path is therefore unreachable for these datasets.
func (c *groupsCollector) applySeriesCap(ctx context.Context, values []metricValue) ([]metricValue, error) {
	sort.Slice(values, func(i, j int) bool { return values[i].name < values[j].name })
	limit := c.cfg.Platform.MaxMetricSeriesPerWindow
	if limit <= 0 {
		return nil, errors.New("platform metric series cap must be positive")
	}
	if len(values) <= limit {
		return values, nil
	}
	slog.WarnContext(ctx, "platform metric series cap dropped account-level metrics", "collector", c.spec.collector, "dropped_series", len(values)-limit)
	return values[:limit], nil
}

var _ collector.WindowCollector = (*groupsCollector)(nil)
