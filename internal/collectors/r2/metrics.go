package r2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const (
	r2TimestampField       = "dimensions.datetimeFiveMinutes"
	r2BucketInterval       = 5 * time.Minute
	r2QueryLimit           = 10000
	r2DefaultSeriesLimit   = 500
	r2MaxResourceNameChars = 128
)

type r2MetricDefinition struct {
	field string
	name  string
	gauge bool
}

type r2ResourceDimension struct {
	field string
	key   string
}

type r2DatasetSpec struct {
	collector string
	dataset   string
	metrics   []r2MetricDefinition
	resource  r2ResourceDimension
}

var r2Datasets = []r2DatasetSpec{
	{
		collector: "r2.bandwidth",
		dataset:   "r2BandwidthUsageAdaptiveGroups",
		metrics: []r2MetricDefinition{
			{field: "sum.bytesDownload", name: semconv.MetricR2BandwidthDownloadBytes},
			{field: "sum.bytesUpload", name: semconv.MetricR2BandwidthUploadBytes},
		},
		resource: r2ResourceDimension{field: "dimensions.bucketName", key: semconv.AttrR2BucketName},
	},
	{
		collector: "r2.catalog_data",
		dataset:   "r2CatalogDataOperationsAdaptiveGroups",
		metrics:   []r2MetricDefinition{{field: "count", name: semconv.MetricR2CatalogDataOperations}},
		resource:  r2ResourceDimension{field: "dimensions.namespaceName", key: semconv.AttrR2CatalogNamespaceName},
	},
	{
		collector: "r2.catalog_maintenance",
		dataset:   "r2CatalogTableMaintenanceAdaptiveGroups",
		metrics:   []r2MetricDefinition{{field: "count", name: semconv.MetricR2CatalogMaintenanceJobs}},
		resource:  r2ResourceDimension{field: "dimensions.namespaceName", key: semconv.AttrR2CatalogNamespaceName},
	},
	{
		collector: "r2.operations",
		dataset:   "r2OperationsAdaptiveGroups",
		metrics:   []r2MetricDefinition{{field: "sum.requests", name: semconv.MetricR2Requests}},
		resource:  r2ResourceDimension{field: "dimensions.bucketName", key: semconv.AttrR2BucketName},
	},
	{
		collector: "r2.storage",
		dataset:   "r2StorageAdaptiveGroups",
		metrics: []r2MetricDefinition{
			{field: "max.payloadSize", name: semconv.MetricR2StoragePayloadBytes, gauge: true},
			{field: "max.objectCount", name: semconv.MetricR2StorageObjects, gauge: true},
		},
		resource: r2ResourceDimension{field: "dimensions.bucketName", key: semconv.AttrR2BucketName},
	},
	{
		collector: "r2.sql",
		dataset:   "r2sqlOperationsAdaptiveGroups",
		metrics:   []r2MetricDefinition{{field: "count", name: semconv.MetricR2SQLQueries}},
		resource:  r2ResourceDimension{field: "dimensions.bucket", key: semconv.AttrR2SQLBucketName},
	},
}

type r2DatasetSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type datasetCollector struct {
	cfg  *config.Config
	api  cfapi.Client
	spec r2DatasetSpec
}

func (c *datasetCollector) Name() string                 { return c.spec.collector }
func (*datasetCollector) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*datasetCollector) Lag() time.Duration             { return 10 * time.Minute }

type r2QueryPlan struct {
	fields           []string
	limit            int
	resourceSelected bool
}

type r2SeriesKey struct {
	metric   string
	resource string
}

type r2MetricPoint struct {
	name        string
	gauge       bool
	value       float64
	sourceTime  time.Time
	resourceKey string
	resource    string
}

func (c *datasetCollector) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid R2 Groups window")
	}
	if c.cfg == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("R2 Groups require a configured Cloudflare account")
	}
	if c.api == nil {
		return from, errors.New("R2 Groups require a Cloudflare client")
	}
	if out == nil {
		return from, errors.New("R2 Groups require a telemetry emitter")
	}
	reader, ok := c.api.(r2DatasetSettingsReader)
	if !ok {
		return from, errors.New("cloudflare client does not expose R2 dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, c.spec.dataset)
	if err != nil {
		return from, fmt.Errorf("read R2 dataset settings: %w", err)
	}
	plan, err := buildR2QueryPlan(c.spec, settings)
	if err != nil {
		return from, err
	}

	completeFrom := ceilR2Bucket(from.UTC())
	completeTo := to.UTC().Truncate(r2BucketInterval)
	if !completeTo.After(from) {
		return from, nil
	}
	if !completeFrom.Before(completeTo) {
		return completeTo, nil
	}

	rows, err := c.queryRows(ctx, completeFrom, completeTo, plan)
	if err != nil {
		return from, fmt.Errorf("query R2 Groups dataset %s: %w", c.spec.dataset, err)
	}
	points, err := c.aggregateRows(rows, completeFrom, completeTo, plan)
	if err != nil {
		return from, err
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}

	limit := c.cfg.Platform.MaxMetricSeriesPerWindow
	if limit <= 0 {
		limit = r2DefaultSeriesLimit
	}
	keys := make([]r2SeriesKey, 0, len(points))
	for key := range points {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].metric != keys[j].metric {
			return keys[i].metric < keys[j].metric
		}
		return keys[i].resource < keys[j].resource
	})
	dropped := len(keys) - limit
	if dropped > 0 {
		keys = keys[:limit]
		slog.Warn("platform metric series dropped", "collector", c.Name(), "dropped", dropped)
	}
	for _, key := range keys {
		point := points[key]
		var attrs []telemetry.Attr
		if point.resourceKey != "" && point.resource != "" {
			attrs = []telemetry.Attr{{Key: point.resourceKey, Value: point.resource}}
		}
		var emitErr error
		if point.gauge {
			emitErr = out.Gauge(ctx, point.name, point.value, attrs...)
		} else {
			emitErr = out.Counter(ctx, point.name, point.value, attrs...)
		}
		if emitErr != nil {
			return from, emitErr
		}
	}
	return completeTo, nil
}

func buildR2QueryPlan(spec r2DatasetSpec, settings cfapi.DatasetSettings) (r2QueryPlan, error) {
	if !settings.Enabled {
		return r2QueryPlan{}, fmt.Errorf("R2 Groups dataset %s is disabled", spec.dataset)
	}
	if settings.MaxNumberOfFields <= 0 {
		return r2QueryPlan{}, fmt.Errorf("R2 Groups dataset %s has an invalid field limit", spec.dataset)
	}
	if settings.MaxPageSize <= 0 {
		return r2QueryPlan{}, fmt.Errorf("R2 Groups dataset %s has no page-size limit", spec.dataset)
	}
	if settings.MaxDuration <= 0 || settings.NotOlderThan <= 0 {
		return r2QueryPlan{}, fmt.Errorf("R2 Groups dataset %s has no duration or retention limit", spec.dataset)
	}

	fields := make([]string, 0, len(spec.metrics)+2)
	for _, metric := range spec.metrics {
		if !r2FieldAvailable(settings.AvailableFields, metric.field) {
			return r2QueryPlan{}, fmt.Errorf("R2 Groups dataset %s is missing required field %s", spec.dataset, metric.field)
		}
		fields = append(fields, metric.field)
	}
	if !r2FieldAvailable(settings.AvailableFields, r2TimestampField) {
		return r2QueryPlan{}, fmt.Errorf("R2 Groups dataset %s is missing required field %s", spec.dataset, r2TimestampField)
	}
	fields = append(fields, r2TimestampField)
	if len(fields) > settings.MaxNumberOfFields {
		return r2QueryPlan{}, fmt.Errorf("R2 Groups dataset %s field limit %d is below its %d required fields", spec.dataset, settings.MaxNumberOfFields, len(fields))
	}
	plan := r2QueryPlan{fields: fields, limit: settings.MaxPageSize}
	if plan.limit > r2QueryLimit {
		plan.limit = r2QueryLimit
	}
	if spec.resource.field != "" && len(plan.fields) < settings.MaxNumberOfFields && r2FieldAvailable(settings.AvailableFields, spec.resource.field) {
		plan.fields = append(plan.fields, spec.resource.field)
		plan.resourceSelected = true
	}
	return plan, nil
}

func r2FieldAvailable(available []string, wanted string) bool {
	wantedLower := strings.ToLower(wanted)
	flattened := strings.ReplaceAll(wantedLower, ".", "_")
	for _, field := range available {
		field = strings.ToLower(strings.TrimSpace(field))
		if field == wantedLower || field == flattened {
			return true
		}
	}
	return false
}

func (c *datasetCollector) queryRows(ctx context.Context, from, to time.Time, plan r2QueryPlan) ([]map[string]any, error) {
	request := cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      c.spec.dataset,
		WantedFields: append([]string(nil), plan.fields...),
		From:         from,
		To:           to,
		Limit:        plan.limit,
	}
	var rows []map[string]any
	err := c.api.Query(ctx, request, &rows)
	saturated := isR2SaturatedError(err, c.spec.dataset)
	if err == nil && len(rows) >= plan.limit {
		saturated = true
	}
	if !saturated {
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			at, parseErr := r2RowTime(row)
			if parseErr != nil {
				return nil, parseErr
			}
			if at.Before(from) || !at.Before(to) {
				return nil, fmt.Errorf("R2 Groups dataset %s returned a bucket outside its half-open query window", c.spec.dataset)
			}
		}
		return rows, nil
	}
	if to.Sub(from) <= time.Minute {
		return nil, fmt.Errorf("R2 Groups dataset %s remains saturated at the irreducible one-minute window", c.spec.dataset)
	}
	mid := from.Add(to.Sub(from) / 2).UTC().Truncate(time.Minute)
	if !mid.After(from) || !mid.Before(to) {
		return nil, fmt.Errorf("R2 Groups dataset %s cannot bisect a saturated interval on minute boundaries", c.spec.dataset)
	}
	left, err := c.queryRows(ctx, from, mid, plan)
	if err != nil {
		return nil, err
	}
	right, err := c.queryRows(ctx, mid, to, plan)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func isR2SaturatedError(err error, dataset string) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "graphql dataset "+strings.ToLower(dataset)+" window ") && strings.Contains(message, " saturated limit ")
}

func (c *datasetCollector) aggregateRows(rows []map[string]any, from, to time.Time, plan r2QueryPlan) (map[r2SeriesKey]*r2MetricPoint, error) {
	points := make(map[r2SeriesKey]*r2MetricPoint)
	for _, row := range rows {
		at, err := r2RowTime(row)
		if err != nil {
			return nil, err
		}
		if at.Before(from) || !at.Before(to) || at.Add(r2BucketInterval).After(to) {
			return nil, fmt.Errorf("R2 Groups dataset %s returned an incomplete or out-of-window bucket", c.spec.dataset)
		}

		resource := ""
		if plan.resourceSelected {
			raw, ok := r2FieldValue(row, c.spec.resource.field)
			if !ok {
				return nil, fmt.Errorf("R2 Groups dataset %s row is missing its selected resource name", c.spec.dataset)
			}
			name, ok := raw.(string)
			if raw == nil {
				name, ok = "", true
			}
			if !ok {
				return nil, fmt.Errorf("R2 Groups dataset %s row has a malformed selected resource name", c.spec.dataset)
			}
			resource = boundedR2Name(name)
		}

		for _, definition := range c.spec.metrics {
			raw, ok := r2FieldValue(row, definition.field)
			if !ok {
				return nil, fmt.Errorf("R2 Groups dataset %s row is missing selected value field %s", c.spec.dataset, definition.field)
			}
			value, ok := r2Number(raw)
			if !ok || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("R2 Groups dataset %s row has an invalid value for %s", c.spec.dataset, definition.field)
			}
			key := r2SeriesKey{metric: definition.name, resource: resource}
			point := points[key]
			if point == nil {
				point = &r2MetricPoint{name: definition.name, gauge: definition.gauge, resourceKey: c.spec.resource.key, resource: resource}
				points[key] = point
			}
			if definition.gauge {
				if at.After(point.sourceTime) || point.sourceTime.IsZero() {
					point.value = value
					point.sourceTime = at
				} else if at.Equal(point.sourceTime) && value > point.value {
					point.value = value
				}
				continue
			}
			point.value += value
			if math.IsNaN(point.value) || math.IsInf(point.value, 0) {
				return nil, fmt.Errorf("R2 Groups dataset %s counter overflow for %s", c.spec.dataset, definition.field)
			}
		}
	}
	return points, nil
}

func r2RowTime(row map[string]any) (time.Time, error) {
	value, ok := r2FieldValue(row, r2TimestampField)
	if !ok {
		return time.Time{}, errors.New("R2 Groups row is missing datetimeFiveMinutes")
	}
	text, ok := value.(string)
	if !ok {
		return time.Time{}, errors.New("R2 Groups row has a malformed datetimeFiveMinutes")
	}
	at, err := time.Parse(time.RFC3339Nano, text)
	if err != nil || !at.Equal(at.Truncate(r2BucketInterval)) {
		return time.Time{}, errors.New("R2 Groups row has a malformed five-minute bucket timestamp")
	}
	return at.UTC(), nil
}

func r2FieldValue(row map[string]any, field string) (any, bool) {
	var current any = row
	for _, part := range strings.Split(field, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func r2Number(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func boundedR2Name(value string) string {
	if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > r2MaxResourceNameChars {
		return ""
	}
	return value
}

func ceilR2Bucket(value time.Time) time.Time {
	value = value.UTC()
	floor := value.Truncate(r2BucketInterval)
	if floor.Equal(value) {
		return floor
	}
	return floor.Add(r2BucketInterval)
}

var _ collector.WindowCollector = (*datasetCollector)(nil)
