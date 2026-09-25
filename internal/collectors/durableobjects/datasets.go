package durableobjects

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
	durableObjectsQueryLimit = 10000
	durableObjectsBucket     = 5 * time.Minute
	durableObjectsTimeField  = "dimensions.datetimeFiveMinutes"
)

type metricKind uint8

const (
	counterMetric metricKind = iota + 1
	gaugeMetric
)

type datasetSpec struct {
	name, dataset, valueField, metric string
	kind                              metricKind
	optionalNamespace                 bool
}

var durableObjectsDatasets = []datasetSpec{
	{
		name:       "durableobjects.invocations",
		dataset:    "durableObjectsInvocationsAdaptiveGroups",
		valueField: "sum.requests",
		metric:     semconv.MetricDurableObjectsRequests,
		kind:       counterMetric,
	},
	{
		name:       "durableobjects.periodic",
		dataset:    "durableObjectsPeriodicGroups",
		valueField: "sum.subrequests",
		metric:     semconv.MetricDurableObjectsSubrequests,
		kind:       counterMetric,
	},
	{
		name:              "durableobjects.sql_storage",
		dataset:           "durableObjectsSqlStorageGroups",
		valueField:        "max.storedBytes",
		metric:            semconv.MetricDurableObjectsSQLStorageBytes,
		kind:              gaugeMetric,
		optionalNamespace: true,
	},
	{
		name:       "durableobjects.subrequests",
		dataset:    "durableObjectsSubrequestsAdaptiveGroups",
		valueField: "sum.requestBodySizeUncached",
		metric:     semconv.MetricDurableObjectsRequestBodyBytes,
		kind:       counterMetric,
	},
}

type datasetSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type datasetCollector struct {
	cfg  *config.Config
	api  cfapi.Client
	spec datasetSpec
}

func newDatasetCollector(cfg *config.Config, api cfapi.Client, spec datasetSpec) *datasetCollector {
	return &datasetCollector{cfg: cfg, api: api, spec: spec}
}

func (c *datasetCollector) Name() string                 { return c.spec.name }
func (*datasetCollector) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*datasetCollector) Lag() time.Duration             { return 10 * time.Minute }

type metricPoint struct {
	name  string
	kind  metricKind
	value float64
}

func (c *datasetCollector) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid Durable Objects metrics window")
	}
	if c.cfg == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("durable objects metrics require a configured Cloudflare account")
	}
	if c.api == nil {
		return from, errors.New("durable objects metrics require a Cloudflare client")
	}
	if out == nil {
		return from, errors.New("durable objects metrics require an emitter")
	}
	seriesLimit := c.cfg.Platform.MaxMetricSeriesPerWindow
	if seriesLimit <= 0 {
		return from, errors.New("platform.max_metric_series_per_window must be positive")
	}
	reader, ok := c.api.(datasetSettingsReader)
	if !ok {
		return from, errors.New("cloudflare client does not expose durable objects dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, c.spec.dataset)
	if err != nil {
		return from, fmt.Errorf("read %s dataset settings: %w", c.spec.name, err)
	}
	if !settings.Enabled {
		return from, fmt.Errorf("durable objects dataset %s is disabled", c.spec.dataset)
	}
	if settings.MaxNumberOfFields <= 0 {
		return from, fmt.Errorf("durable objects dataset %s has an invalid field limit", c.spec.dataset)
	}
	if settings.MaxPageSize <= 0 {
		return from, fmt.Errorf("durable objects dataset %s has an invalid page-size limit", c.spec.dataset)
	}
	if settings.MaxDuration <= 0 || settings.NotOlderThan <= 0 {
		return from, fmt.Errorf("durable objects dataset %s is missing its duration or retention limit", c.spec.dataset)
	}
	for _, field := range []string{c.spec.valueField, durableObjectsTimeField} {
		if !availableField(settings.AvailableFields, field) {
			return from, fmt.Errorf("durable objects dataset %s is missing required field %s", c.spec.dataset, field)
		}
	}

	fields := []string{c.spec.valueField, durableObjectsTimeField}
	if len(fields) > settings.MaxNumberOfFields {
		return from, fmt.Errorf("durable objects dataset %s needs %d required fields, limit %d", c.spec.dataset, len(fields), settings.MaxNumberOfFields)
	}
	if c.spec.optionalNamespace && len(fields) < settings.MaxNumberOfFields && availableField(settings.AvailableFields, "dimensions.namespaceName") {
		fields = append(fields, "dimensions.namespaceName")
	}
	limit := settings.MaxPageSize
	if limit > durableObjectsQueryLimit {
		limit = durableObjectsQueryLimit
	}
	request := cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      c.spec.dataset,
		WantedFields: fields,
		From:         from,
		To:           to,
		Limit:        limit,
	}

	completeFrom := ceilBucket(from)
	completeTo := floorBucket(to)
	if completeTo.Before(from) {
		completeTo = from
	}
	if !completeFrom.Before(completeTo) {
		return completeTo, nil
	}
	rows, err := c.queryRows(ctx, request, completeFrom, completeTo)
	if err != nil {
		return from, fmt.Errorf("query %s: %w", c.spec.dataset, err)
	}
	value, found, err := c.aggregate(rows, completeFrom, completeTo)
	if err != nil {
		return from, fmt.Errorf("aggregate %s: %w", c.spec.dataset, err)
	}
	if !found {
		return completeTo, nil
	}
	points := capMetricSeries(c.spec.name, []metricPoint{{name: c.spec.metric, kind: c.spec.kind, value: value}}, seriesLimit, slog.Default())
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

func (c *datasetCollector) queryRows(ctx context.Context, request cfapi.GraphQLRequest, from, to time.Time) ([]map[string]any, error) {
	request.From = from
	request.To = to
	var rows []map[string]any
	err := c.api.Query(ctx, request, &rows)
	if err == nil && len(rows) < request.Limit {
		return rows, nil
	}
	if err == nil {
		err = saturatedError(request)
	}
	if !isSaturated(err, request.Dataset) {
		return nil, err
	}
	split, ok := splitAtBucket(from, to)
	if !ok {
		return nil, err
	}
	left, err := c.queryRows(ctx, request, from, split)
	if err != nil {
		return nil, err
	}
	right, err := c.queryRows(ctx, request, split, to)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func saturatedError(request cfapi.GraphQLRequest) error {
	return fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
}

func isSaturated(err error, dataset string) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "GraphQL dataset "+dataset+" window ") && strings.Contains(message, " saturated limit ")
}

func splitAtBucket(from, to time.Time) (time.Time, bool) {
	if to.Sub(from) <= durableObjectsBucket {
		return time.Time{}, false
	}
	split := from.Add(to.Sub(from) / 2).Truncate(durableObjectsBucket)
	if !split.Before(to) {
		return time.Time{}, false
	}
	return split, split.After(from)
}

func (c *datasetCollector) aggregate(rows []map[string]any, from, to time.Time) (float64, bool, error) {
	var total, maximum float64
	var latest time.Time
	found := false
	for index, row := range rows {
		bucket, err := rowBucket(row)
		if err != nil {
			return 0, false, fmt.Errorf("row %d: %w", index, err)
		}
		if bucket.Before(from) || !bucket.Before(to) || bucket.Add(durableObjectsBucket).After(to) {
			continue
		}
		value, err := rowMetricValue(row, c.spec.valueField)
		if err != nil {
			return 0, false, fmt.Errorf("row %d field %s: %w", index, c.spec.valueField, err)
		}
		if c.spec.kind == counterMetric {
			total += value
			if math.IsInf(total, 0) || math.IsNaN(total) {
				return 0, false, errors.New("counter aggregation is not finite")
			}
			found = true
			continue
		}
		if !found || bucket.After(latest) {
			latest, maximum, found = bucket, value, true
		} else if bucket.Equal(latest) && value > maximum {
			maximum = value
		}
	}
	if c.spec.kind == counterMetric {
		return total, found, nil
	}
	return maximum, found, nil
}

func rowBucket(row map[string]any) (time.Time, error) {
	dimensions, ok := row["dimensions"].(map[string]any)
	if !ok {
		return time.Time{}, errors.New("missing dimensions")
	}
	value, ok := dimensions["datetimeFiveMinutes"]
	if !ok {
		return time.Time{}, errors.New("missing dimensions.datetimeFiveMinutes")
	}
	var bucket time.Time
	switch value := value.(type) {
	case time.Time:
		bucket = value
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}, errors.New("invalid dimensions.datetimeFiveMinutes")
		}
		bucket = parsed
	default:
		return time.Time{}, errors.New("invalid dimensions.datetimeFiveMinutes")
	}
	bucket = bucket.UTC()
	if !bucket.Equal(bucket.Truncate(durableObjectsBucket)) {
		return time.Time{}, errors.New("datetimeFiveMinutes is not aligned to a five-minute bucket")
	}
	return bucket, nil
}

func rowMetricValue(row map[string]any, field string) (float64, error) {
	aggregation, sourceField, ok := strings.Cut(field, ".")
	if !ok {
		return 0, errors.New("invalid configured metric field")
	}
	values, ok := row[aggregation].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("missing %s values", aggregation)
	}
	raw, ok := values[sourceField]
	if !ok {
		return 0, fmt.Errorf("missing %s.%s", aggregation, sourceField)
	}
	value, ok := finiteNonnegativeNumber(raw)
	if !ok {
		return 0, errors.New("metric value is not a finite nonnegative number")
	}
	return value, nil
}

func finiteNonnegativeNumber(value any) (float64, bool) {
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

func availableField(available []string, wanted string) bool {
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

func floorBucket(value time.Time) time.Time {
	return value.UTC().Truncate(durableObjectsBucket)
}

func ceilBucket(value time.Time) time.Time {
	floor := floorBucket(value)
	if floor.Equal(value.UTC()) {
		return floor
	}
	return floor.Add(durableObjectsBucket)
}

func capMetricSeries(collectorName string, points []metricPoint, limit int, logger *slog.Logger) []metricPoint {
	sort.Slice(points, func(i, j int) bool { return points[i].name < points[j].name })
	if len(points) <= limit {
		return points
	}
	dropped := len(points) - limit
	logger.Warn("platform metric series dropped", "collector", collectorName, "dropped", dropped)
	return points[:limit]
}

var _ collector.WindowCollector = (*datasetCollector)(nil)
