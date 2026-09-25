package queues

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
	queueQueryLimit         = 10000
	queueBucketDuration     = 5 * time.Minute
	queueMinimumQueryWindow = 5 * time.Minute
	queueDefaultSeriesLimit = 500
	queueTimestampField     = "dimensions.datetimeFiveMinutes"
	queueIDField            = "dimensions.queueId"
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
	collector  string
	dataset    string
	metrics    []metricSpec
	queueIDMax bool
}

var datasets = []datasetSpec{
	{
		collector: "queues.backlog",
		dataset:   "queueBacklogAdaptiveGroups",
		metrics: []metricSpec{
			{name: semconv.MetricQueuesBacklogMessages, field: "avg.messages", kind: gaugeMetric},
			{name: semconv.MetricQueuesBacklogBytes, field: "avg.bytes", kind: gaugeMetric},
		},
		queueIDMax: true,
	},
	{
		collector:  "queues.consumer",
		dataset:    "queueConsumerMetricsAdaptiveGroups",
		metrics:    []metricSpec{{name: semconv.MetricQueuesConsumerConcurrency, field: "avg.concurrency", kind: gaugeMetric}},
		queueIDMax: true,
	},
	{
		collector:  "queues.delayed_backlog",
		dataset:    "queueDelayedBacklogAdaptiveGroups",
		metrics:    []metricSpec{{name: semconv.MetricQueuesDelayedBacklogMessages, field: "avg.messages", kind: gaugeMetric}},
		queueIDMax: true,
	},
	{
		collector: "queues.message_operations",
		dataset:   "queueMessageOperationsAdaptiveGroups",
		metrics: []metricSpec{
			{name: semconv.MetricQueuesMessageOperations, field: "count", kind: counterMetric},
			{name: semconv.MetricQueuesBillableOperations, field: "sum.billableOperations", kind: counterMetric},
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

type queryPlan struct {
	fields []string
	limit  int
}

type metricValue struct {
	name  string
	value float64
	kind  metricKind
}

// Register installs each enabled account-level Queue Groups window collector.
func Register(deps collector.Deps) {
	if deps.Config == nil || deps.Registry == nil {
		return
	}
	for _, spec := range datasets {
		cfg := deps.Config.Collector(spec.collector)
		if cfg.Enabled {
			deps.Registry.RegisterWindow(newGroupsCollector(deps.Config, deps.API, spec), cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
		}
	}
}

func newGroupsCollector(cfg *config.Config, api cfapi.Client, spec datasetSpec) *groupsCollector {
	return &groupsCollector{cfg: cfg, api: api, spec: spec}
}

func (c *groupsCollector) Name() string                 { return c.spec.collector }
func (*groupsCollector) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*groupsCollector) Lag() time.Duration             { return 10 * time.Minute }

func (c *groupsCollector) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid Queue Groups window")
	}
	if c.cfg == nil || c.api == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("queue Groups collector requires a configured Cloudflare account and API")
	}
	if out == nil {
		return from, errors.New("queue Groups collector requires a telemetry emitter")
	}
	reader, ok := c.api.(settingsReader)
	if !ok {
		return from, errors.New("cloudflare client does not expose Queue Groups dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, c.spec.dataset)
	if err != nil {
		return from, fmt.Errorf("read Queue Groups settings for %s: %w", c.spec.dataset, err)
	}
	plan, err := buildQueryPlan(c.spec, settings)
	if err != nil {
		return from, err
	}

	completeFrom := ceilBucket(from.UTC())
	completeTo := floorBucket(to.UTC())
	if !completeFrom.Before(completeTo) {
		return from, nil
	}
	request := cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      c.spec.dataset,
		WantedFields: append([]string(nil), plan.fields...),
		From:         completeFrom,
		To:           completeTo,
		Limit:        plan.limit,
	}
	rows, err := c.queryRows(ctx, request)
	if err != nil {
		return from, fmt.Errorf("query Queue Groups dataset %s: %w", c.spec.dataset, err)
	}
	values, err := c.aggregate(rows, completeFrom, completeTo)
	if err != nil {
		return from, err
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}
	points := capSeries(c.Name(), values, c.cfg.Platform.MaxMetricSeriesPerWindow)
	for _, point := range points {
		var emitErr error
		if point.kind == gaugeMetric {
			emitErr = out.Gauge(ctx, point.name, point.value)
		} else {
			emitErr = out.Counter(ctx, point.name, point.value)
		}
		if emitErr != nil {
			return from, emitErr
		}
	}
	return completeTo, nil
}

func buildQueryPlan(spec datasetSpec, settings cfapi.DatasetSettings) (queryPlan, error) {
	if !settings.Enabled {
		return queryPlan{}, fmt.Errorf("queue Groups dataset %s is disabled", spec.dataset)
	}
	if settings.MaxNumberOfFields <= 0 {
		return queryPlan{}, fmt.Errorf("queue Groups dataset %s has an invalid field limit", spec.dataset)
	}
	if settings.MaxPageSize <= 0 {
		return queryPlan{}, fmt.Errorf("queue Groups dataset %s has no page-size limit", spec.dataset)
	}
	if settings.MaxDuration <= 0 || settings.NotOlderThan <= 0 {
		return queryPlan{}, fmt.Errorf("queue Groups dataset %s has no duration or retention limit", spec.dataset)
	}

	fields := make([]string, 0, len(spec.metrics)+2)
	for _, metric := range spec.metrics {
		if !availableField(settings.AvailableFields, metric.field) {
			return queryPlan{}, fmt.Errorf("queue Groups dataset %s is missing required field %s", spec.dataset, metric.field)
		}
		fields = append(fields, metric.field)
	}
	if !availableField(settings.AvailableFields, queueTimestampField) {
		return queryPlan{}, fmt.Errorf("queue Groups dataset %s is missing required field %s", spec.dataset, queueTimestampField)
	}
	fields = append(fields, queueTimestampField)
	if spec.queueIDMax {
		if !availableField(settings.AvailableFields, queueIDField) {
			return queryPlan{}, fmt.Errorf("queue Groups dataset %s is missing required field %s", spec.dataset, queueIDField)
		}
		fields = append(fields, queueIDField)
	}
	if len(fields) > settings.MaxNumberOfFields {
		return queryPlan{}, fmt.Errorf("queue Groups dataset %s field limit %d is below its %d required fields", spec.dataset, settings.MaxNumberOfFields, len(fields))
	}
	return queryPlan{fields: fields, limit: min(queueQueryLimit, settings.MaxPageSize)}, nil
}

func availableField(available []string, wanted string) bool {
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	flattened := strings.ReplaceAll(wanted, ".", "_")
	for _, field := range available {
		field = strings.ToLower(strings.TrimSpace(field))
		if field == wanted || field == flattened {
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
	}
	if !saturated {
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			bucket, parseErr := rowBucket(row)
			if parseErr != nil {
				return nil, parseErr
			}
			if bucket.Before(request.From) || !bucket.Before(request.To) || bucket.Add(queueBucketDuration).After(request.To) {
				return nil, fmt.Errorf("queue Groups dataset %s returned a bucket outside its half-open query window", c.spec.dataset)
			}
		}
		return rows, nil
	}
	if request.To.Sub(request.From) <= queueMinimumQueryWindow {
		return nil, fmt.Errorf("queue Groups dataset %s remains saturated at the irreducible five-minute bucket", c.spec.dataset)
	}
	mid := request.From.Add(request.To.Sub(request.From) / 2).UTC().Truncate(queueBucketDuration)
	if !mid.After(request.From) || !mid.Before(request.To) {
		return nil, fmt.Errorf("queue Groups dataset %s cannot bisect a saturated interval on five-minute boundaries", c.spec.dataset)
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
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "graphql dataset "+strings.ToLower(dataset)+" window ") && strings.Contains(message, " saturated limit ")
}

func (c *groupsCollector) aggregate(rows []map[string]any, from, to time.Time) ([]metricValue, error) {
	counters := make(map[string]float64, len(c.spec.metrics))
	counterSeen := make(map[string]bool, len(c.spec.metrics))
	perQueue := map[string]map[string]float64{}
	var latestBucket time.Time

	for _, row := range rows {
		bucket, err := rowBucket(row)
		if err != nil {
			return nil, fmt.Errorf("queue Groups dataset %s returned an invalid five-minute bucket", c.spec.dataset)
		}
		if bucket.Before(from) || !bucket.Before(to) || bucket.Add(queueBucketDuration).After(to) {
			return nil, fmt.Errorf("queue Groups dataset %s returned an incomplete or out-of-window bucket", c.spec.dataset)
		}
		values := make(map[string]float64, len(c.spec.metrics))
		for _, metric := range c.spec.metrics {
			raw, ok := fieldValue(row, metric.field)
			if !ok {
				return nil, fmt.Errorf("queue Groups dataset %s row is missing selected value field %s", c.spec.dataset, metric.field)
			}
			value, ok := nonnegativeNumber(raw)
			if !ok {
				return nil, fmt.Errorf("queue Groups dataset %s row has an invalid value for %s", c.spec.dataset, metric.field)
			}
			values[metric.name] = value
		}

		if c.spec.queueIDMax {
			raw, ok := fieldValue(row, queueIDField)
			queue, valid := queueIdentifier(raw)
			if !ok || !valid {
				return nil, fmt.Errorf("queue Groups dataset %s row is missing a valid internal queue grouping value", c.spec.dataset)
			}
			if bucket.After(latestBucket) {
				latestBucket = bucket
				clear(perQueue)
			}
			if bucket.Equal(latestBucket) {
				queueValues := perQueue[queue]
				if queueValues == nil {
					queueValues = make(map[string]float64, len(values))
					perQueue[queue] = queueValues
				}
				for name, value := range values {
					if old, exists := queueValues[name]; !exists || value > old {
						queueValues[name] = value
					}
				}
			}
			continue
		}

		for _, metric := range c.spec.metrics {
			if metric.kind != counterMetric {
				continue
			}
			counters[metric.name] += values[metric.name]
			if math.IsNaN(counters[metric.name]) || math.IsInf(counters[metric.name], 0) {
				return nil, fmt.Errorf("queue Groups dataset %s counter overflow for %s", c.spec.dataset, metric.field)
			}
			counterSeen[metric.name] = true
		}
	}

	points := make([]metricValue, 0, len(c.spec.metrics))
	for _, metric := range c.spec.metrics {
		if metric.kind == counterMetric {
			if counterSeen[metric.name] {
				points = append(points, metricValue{name: metric.name, value: counters[metric.name], kind: counterMetric})
			}
			continue
		}
		var maximum float64
		seen := false
		for _, queueValues := range perQueue {
			if value, ok := queueValues[metric.name]; ok && (!seen || value > maximum) {
				maximum = value
				seen = true
			}
		}
		if seen {
			points = append(points, metricValue{name: metric.name, value: maximum, kind: gaugeMetric})
		}
	}
	return points, nil
}

func rowBucket(row map[string]any) (time.Time, error) {
	raw, ok := fieldValue(row, queueTimestampField)
	if !ok {
		return time.Time{}, errors.New("missing datetimeFiveMinutes")
	}
	var bucket time.Time
	switch value := raw.(type) {
	case time.Time:
		bucket = value.UTC()
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}, err
		}
		bucket = parsed.UTC()
	default:
		return time.Time{}, errors.New("invalid datetimeFiveMinutes type")
	}
	if !bucket.Equal(bucket.Truncate(queueBucketDuration)) {
		return time.Time{}, errors.New("datetimeFiveMinutes is not aligned to a five-minute bucket")
	}
	return bucket, nil
}

func fieldValue(row map[string]any, field string) (any, bool) {
	var value any = row
	for _, part := range strings.Split(field, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func nonnegativeNumber(value any) (float64, bool) {
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case float32:
		number = float64(value)
	case int:
		number = float64(value)
	case int8:
		number = float64(value)
	case int16:
		number = float64(value)
	case int32:
		number = float64(value)
	case int64:
		number = float64(value)
	case uint:
		number = float64(value)
	case uint8:
		number = float64(value)
	case uint16:
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

func queueIdentifier(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		return trimmed, trimmed != ""
	case json.Number:
		return value.String(), true
	case int:
		return strconv.Itoa(value), true
	case int32:
		return strconv.FormatInt(int64(value), 10), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
			return "", false
		}
		return strconv.FormatFloat(value, 'f', 0, 64), true
	default:
		return "", false
	}
}

func capSeries(collectorName string, values []metricValue, limit int) []metricValue {
	if limit <= 0 {
		limit = queueDefaultSeriesLimit
	}
	sort.Slice(values, func(i, j int) bool { return values[i].name < values[j].name })
	dropped := len(values) - limit
	if dropped > 0 {
		values = values[:limit]
		slog.Warn("platform metric series dropped", "collector", collectorName, "dropped", dropped)
	}
	return values
}

func floorBucket(value time.Time) time.Time {
	return value.UTC().Truncate(queueBucketDuration)
}

func ceilBucket(value time.Time) time.Time {
	value = value.UTC()
	floor := floorBucket(value)
	if floor.Equal(value) {
		return floor
	}
	return floor.Add(queueBucketDuration)
}

var _ collector.WindowCollector = (*groupsCollector)(nil)
