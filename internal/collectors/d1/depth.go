package d1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type depthField struct {
	field, metric, statistic string
	divisor                  float64
}

func analyticsDepthFields() []depthField {
	fields := []depthField{{field: "sum.rowsRead", metric: semconv.MetricD1RowsRead, divisor: 1}, {field: "sum.rowsWritten", metric: semconv.MetricD1RowsWritten, divisor: 1}}
	for _, stat := range []string{"p50", "p75", "p99", "p999"} {
		suffix := strings.ToUpper(stat[:1]) + stat[1:]
		fields = append(fields, depthField{"quantiles.queryBatchTimeMs" + suffix, semconv.MetricD1QueryBatchTime, stat, 1000}, depthField{"quantiles.queryBatchResponseBytes" + suffix, semconv.MetricD1QueryBatchResponseSize, stat, 1})
	}
	return fields
}

func (c *groupsCollector) collectDepth(ctx context.Context, request cfapi.GraphQLRequest, settings cfapi.DatasetSettings) ([]metricValue, error) {
	if c.Name() != "d1.analytics" {
		return nil, nil
	}
	capacity := settings.MaxNumberOfFields - 1
	if capacity <= 0 {
		return nil, nil
	}
	var fields []depthField
	for _, f := range analyticsDepthFields() {
		if hasAvailableField(settings.AvailableFields, f.field) {
			fields = append(fields, f)
		}
	}
	var points []metricValue
	for len(fields) > 0 {
		n := min(capacity, len(fields))
		chunk := fields[:n]
		fields = fields[n:]
		request.WantedFields = []string{"dimensions.datetimeFiveMinutes"}
		for _, f := range chunk {
			request.WantedFields = append(request.WantedFields, f.field)
		}
		rows, err := c.queryDepthRows(ctx, request, settings)
		if err != nil {
			return nil, err
		}
		seen := map[time.Time]bool{}
		var latest time.Time
		for _, row := range rows {
			bucket, err := rowBucket(row)
			if err != nil {
				return nil, err
			}
			if seen[bucket] {
				return nil, errors.New("duplicate ambiguous D1 account bucket")
			}
			seen[bucket] = true
			if bucket.After(latest) {
				latest = bucket
			}
		}
		for _, f := range chunk {
			var total float64
			found := false
			for _, row := range rows {
				bucket, _ := rowBucket(row)
				if f.statistic != "" && !bucket.Equal(latest) {
					continue
				}
				value, exists, err := depthNumber(row, f.field)
				if err != nil {
					return nil, err
				}
				if !exists {
					continue
				}
				total += value
				found = true
				if math.IsInf(total, 0) {
					return nil, errors.New("D1 depth aggregation overflow")
				}
			}
			if !found {
				continue
			}
			point := metricValue{name: f.metric, value: total / f.divisor, kind: counterMetric}
			if f.statistic != "" {
				point.kind = gaugeMetric
				point.attrs = []telemetry.Attr{{Key: semconv.AttrStatistic, Value: f.statistic}}
			}
			points = append(points, point)
		}
	}
	return points, nil
}

func depthNumber(row map[string]any, field string) (float64, bool, error) {
	part, key, _ := strings.Cut(field, ".")
	group, _ := row[part].(map[string]any)
	raw := group[key]
	if raw == nil {
		return 0, false, nil
	}
	if n, ok := numericField(row, field); ok {
		return n, true, nil
	}
	if n, ok := raw.(float64); ok && n < 0 && !math.IsInf(n, 0) {
		return 0, false, nil
	}
	return 0, false, fmt.Errorf("invalid optional D1 field %s", field)
}

func (c *groupsCollector) queryDepthRows(ctx context.Context, request cfapi.GraphQLRequest, settings cfapi.DatasetSettings) ([]map[string]any, error) {
	duration := time.Duration(settings.MaxDuration) * time.Second / d1BucketDuration * d1BucketDuration
	if duration < d1BucketDuration || settings.NotOlderThan <= 0 {
		return nil, errors.New("D1 depth requires complete-bucket duration and retention limits")
	}
	var all []map[string]any
	for start := request.From; start.Before(request.To); {
		end := start.Add(duration)
		if end.After(request.To) {
			end = request.To
		}
		leaf := request
		leaf.From, leaf.To = start, end
		rows, err := c.queryRows(ctx, leaf)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			bucket, err := rowBucket(row)
			if err != nil {
				return nil, err
			}
			if !bucket.Equal(bucket.Truncate(d1BucketDuration)) || bucket.Before(start) || !bucket.Before(end) || bucket.Add(d1BucketDuration).After(end) {
				return nil, errors.New("D1 depth returned an incomplete or out-of-window bucket")
			}
		}
		all = append(all, rows...)
		start = end
	}
	return all, nil
}

func metricValueKey(point metricValue) string {
	data, _ := json.Marshal([]any{point.name, point.attrs})
	return string(data)
}
