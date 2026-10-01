package durableobjects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type depthField struct {
	field, metric, statistic string
	divisor                  float64
}

func invocationDepthFields() []depthField {
	fields := []depthField{{field: "sum.errors", metric: semconv.MetricDurableObjectsErrors, divisor: 1}}
	for _, stat := range []string{"p50", "p75", "p99", "p999"} {
		suffix := strings.ToUpper(stat[:1]) + stat[1:]
		fields = append(fields, depthField{"quantiles.wallTime" + suffix, semconv.MetricDurableObjectsWallTime, stat, 1e6}, depthField{"quantiles.responseBodySize" + suffix, semconv.MetricDurableObjectsResponseSize, stat, 1})
	}
	return fields
}

// Each selection has exactly script/bucket grouping, never status or object ID.
// Separate bounded field chunks have identical grouping but are not numerically
// combined: every quantile remains the source's latest complete bucket value.
func (c *datasetCollector) collectDepth(ctx context.Context, request cfapi.GraphQLRequest, settings cfapi.DatasetSettings, from, to time.Time) ([]metricPoint, error) {
	if c.Name() != "durableobjects.invocations" || !availableField(settings.AvailableFields, "dimensions.scriptName") {
		return nil, nil
	}
	dims := []string{durableObjectsTimeField, "dimensions.scriptName"}
	capacity := settings.MaxNumberOfFields - len(dims)
	if capacity <= 0 {
		return nil, nil
	}
	var fields []depthField
	for _, f := range invocationDepthFields() {
		if availableField(settings.AvailableFields, f.field) {
			fields = append(fields, f)
		}
	}
	var points []metricPoint
	for len(fields) > 0 {
		n := min(capacity, len(fields))
		chunk := fields[:n]
		fields = fields[n:]
		request.WantedFields = append([]string(nil), dims...)
		for _, f := range chunk {
			request.WantedFields = append(request.WantedFields, f.field)
		}
		rows, err := c.queryDepthRows(ctx, request, settings, from, to)
		if err != nil {
			return nil, err
		}
		latest := map[string]time.Time{}
		seen := map[string]bool{}
		for _, row := range rows {
			for _, f := range chunk {
				if f.statistic == "" {
					if _, err := depthCount(row, f.field); err != nil {
						return nil, err
					}
				}
			}
			bucket, err := rowBucket(row)
			if err != nil {
				return nil, err
			}
			d := row["dimensions"].(map[string]any)
			script, ok := d["scriptName"].(string)
			script = strings.TrimSpace(script)
			if !ok || script == "" || utf8.RuneCountInString(script) > 128 {
				continue
			}
			keyBytes, _ := json.Marshal([]string{script, bucket.Format(time.RFC3339Nano)})
			key := string(keyBytes)
			if seen[key] {
				return nil, errors.New("duplicate ambiguous Durable Objects script bucket")
			}
			seen[key] = true
			if bucket.After(latest[script]) {
				latest[script] = bucket
			}
		}
		totals := map[string]float64{}
		counterSeen := map[string]bool{}
		for _, row := range rows {
			bucket, _ := rowBucket(row)
			d := row["dimensions"].(map[string]any)
			script, _ := d["scriptName"].(string)
			script = strings.TrimSpace(script)
			if _, ok := latest[script]; !ok {
				continue
			}
			for _, f := range chunk {
				var value float64
				var exists bool
				var err error
				if f.statistic == "" {
					value, err = depthCount(row, f.field)
					exists = err == nil
				} else {
					value, exists, err = depthNumber(row, f.field)
				}
				if err != nil {
					return nil, err
				}
				if !exists {
					continue
				}
				attrs := []telemetry.Attr{{Key: semconv.AttrWorkersScriptName, Value: script}}
				if f.statistic == "" {
					totals[script] += value
					if math.IsInf(totals[script], 0) {
						return nil, errors.New("durable objects error count overflow")
					}
					counterSeen[script] = true
				} else if bucket.Equal(latest[script]) {
					attrs = append(attrs, telemetry.Attr{Key: semconv.AttrStatistic, Value: f.statistic})
					points = append(points, metricPoint{name: f.metric, kind: gaugeMetric, value: value / f.divisor, attrs: attrs})
				}
			}
		}
		for script, value := range totals {
			if counterSeen[script] {
				points = append(points, metricPoint{name: semconv.MetricDurableObjectsErrors, kind: counterMetric, value: value, attrs: []telemetry.Attr{{Key: semconv.AttrWorkersScriptName, Value: script}}})
			}
		}
	}
	return points, nil
}

func depthNumber(row map[string]any, field string) (float64, bool, error) {
	part, key, _ := strings.Cut(field, ".")
	group, _ := row[part].(map[string]any)
	raw := group[key]
	if n, ok := raw.(json.Number); ok {
		value, err := n.Float64()
		if err != nil {
			return 0, false, fmt.Errorf("invalid optional Durable Objects field %s: %w", field, err)
		}
		raw = value
	}
	if raw == nil {
		return 0, false, nil
	}
	if n, ok := finiteNonnegativeNumber(raw); ok {
		return n, true, nil
	}
	if n, ok := raw.(float64); ok && n < 0 && !math.IsInf(n, 0) {
		return 0, false, nil
	}
	return 0, false, fmt.Errorf("invalid optional Durable Objects field %s", field)
}

func (c *datasetCollector) queryDepthRows(ctx context.Context, request cfapi.GraphQLRequest, settings cfapi.DatasetSettings, from, to time.Time) ([]map[string]any, error) {
	duration := time.Duration(settings.MaxDuration) * time.Second / durableObjectsBucket * durableObjectsBucket
	if duration < durableObjectsBucket {
		return nil, errors.New("durable objects duration limit cannot cover a complete bucket")
	}
	var all []map[string]any
	for start := from; start.Before(to); {
		end := start.Add(duration)
		if end.After(to) {
			end = to
		}
		rows, err := c.queryRowsMode(ctx, request, start, end, true)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			bucket, err := rowBucket(row)
			if err != nil {
				return nil, err
			}
			if bucket.Before(start) || !bucket.Before(end) || bucket.Add(durableObjectsBucket).After(end) {
				return nil, errors.New("durable objects depth returned an incomplete or out-of-window bucket")
			}
		}
		all = append(all, rows...)
		start = end
	}
	return all, nil
}

// Selected additive counts are required unsigned integers, unlike optional statistics.
func depthCount(row map[string]any, field string) (float64, error) {
	part, key, _ := strings.Cut(field, ".")
	group, _ := row[part].(map[string]any)
	n, ok := group[key].(json.Number)
	if !ok {
		return 0, fmt.Errorf("missing or invalid selected count %s", field)
	}
	integer, ok := new(big.Rat).SetString(n.String())
	if !ok || !integer.IsInt() || !integer.Num().IsUint64() {
		return 0, fmt.Errorf("invalid unsigned integral count %s", field)
	}
	// Validate exactly before the existing float counter representation rounds large counts.
	value, err := n.Float64()
	return value, err
}

func (c *datasetCollector) queryDepthLeaf(ctx context.Context, request cfapi.GraphQLRequest) ([]map[string]any, error) {
	batch, ok := c.api.(cfapi.GraphQLBatchQuerier)
	if !ok {
		return nil, errors.New("depth collection requires raw GraphQL batch queries")
	}
	result, err := batch.QueryBatch(ctx, []cfapi.GraphQLBatchSelection{{Alias: "depth", Request: request}})
	if err != nil {
		return nil, err
	}
	raw := bytes.TrimSpace(result["depth"])
	if len(raw) == 0 || raw[0] != '[' {
		return nil, errors.New("depth dataset must be a nonnull row array")
	}
	var rows []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func metricPointKey(point metricPoint) string {
	data, _ := json.Marshal([]any{point.name, point.attrs})
	return string(data)
}
