package queues

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
	field, metric string
	divisor       float64
}

func (c *groupsCollector) collectDepth(ctx context.Context, request cfapi.GraphQLRequest, settings cfapi.DatasetSettings) ([]metricValue, error) {
	if c.Name() != "queues.message_operations" {
		return nil, nil
	}
	var points []metricValue
	// Root's queue-filter-proof.json validated the scalar string ReadMessage.
	// queueId is internal grouping only; no action/outcome split of averages.
	dims := []string{queueTimestampField, queueIDField}
	capacity := settings.MaxNumberOfFields - len(dims)
	if availableField(settings.AvailableFields, queueIDField) && capacity > 0 {
		var fields []depthField
		for _, f := range []depthField{{"avg.lagTime", semconv.MetricQueuesMessageLag, 1000}, {"avg.retryCount", semconv.MetricQueuesMessageRetries, 1}} {
			if availableField(settings.AvailableFields, f.field) {
				fields = append(fields, f)
			}
		}
		for len(fields) > 0 {
			n := min(capacity, len(fields))
			chunk := fields[:n]
			fields = fields[n:]
			request.WantedFields = append([]string(nil), dims...)
			for _, f := range chunk {
				request.WantedFields = append(request.WantedFields, f.field)
			}
			request.Filter = map[string]any{"actionType": "ReadMessage"}
			rows, err := c.queryDepthRows(ctx, request, settings)
			if err != nil {
				return nil, err
			}
			var latest time.Time
			seen := map[string]bool{}
			for _, row := range rows {
				bucket, err := rowBucket(row)
				if err != nil {
					return nil, err
				}
				raw, _ := fieldValue(row, queueIDField)
				queue, ok := queueIdentifier(raw)
				if !ok {
					return nil, errors.New("missing internal Queue depth grouping")
				}
				keyBytes, _ := json.Marshal([]string{queue, bucket.Format(time.RFC3339Nano)})
				key := string(keyBytes)
				if seen[key] {
					return nil, errors.New("duplicate ambiguous Queue average bucket")
				}
				seen[key] = true
				if bucket.After(latest) {
					latest = bucket
				}
			}
			for _, f := range chunk {
				var maximum float64
				found := false
				for _, row := range rows {
					bucket, _ := rowBucket(row)
					if !bucket.Equal(latest) {
						continue
					}
					value, exists, err := depthNumber(row, f.field)
					if err != nil {
						return nil, err
					}
					if exists && (!found || value > maximum) {
						maximum = value
						found = true
					}
				}
				if found {
					points = append(points, metricValue{name: f.metric, kind: gaugeMetric, value: maximum / f.divisor})
				}
			}
		}
	}
	// The old aggregate is never reinterpreted; this independent selection groups
	// billable sums by exactly the three frozen dimensions plus complete bucket.
	dims = []string{queueTimestampField, "dimensions.actionType", "dimensions.consumerType", "dimensions.outcome"}
	fields := append(append([]string(nil), dims...), "sum.billableOperations")
	entitled := len(fields) <= settings.MaxNumberOfFields
	for _, f := range fields {
		entitled = entitled && availableField(settings.AvailableFields, f)
	}
	if entitled {
		request.WantedFields = fields
		request.Filter = nil
		rows, err := c.queryDepthRows(ctx, request, settings)
		if err != nil {
			return nil, err
		}
		totals := map[string]metricValue{}
		seen := map[string]bool{}
		for _, row := range rows {
			value, err := depthCount(row, "sum.billableOperations")
			if err != nil {
				return nil, err
			}
			var attrs []telemetry.Attr
			valid := true
			for i, f := range dims[1:] {
				raw, _ := fieldValue(row, f)
				value, ok := raw.(string)
				// Empty consumerType is the source's inapplicable value for send
				// operations. Preserve source strings rather than inventing a label.
				if !ok || utf8.RuneCountInString(value) > 128 {
					valid = false
					break
				}
				attrs = append(attrs, telemetry.Attr{Key: []string{semconv.AttrQueuesActionType, semconv.AttrQueuesConsumerType, semconv.AttrQueuesOutcome}[i], Value: value})
			}
			if !valid {
				continue
			}
			p := metricValue{name: semconv.MetricQueuesBillableOperationsByAction, kind: counterMetric, attrs: attrs}
			key := metricValueKey(p)
			bucket, _ := rowBucket(row)
			rowKey := key + bucket.Format(time.RFC3339Nano)
			if seen[rowKey] {
				return nil, errors.New("duplicate Queue action bucket")
			}
			seen[rowKey] = true
			p.value = totals[key].value + value
			if math.IsInf(p.value, 0) {
				return nil, errors.New("queue action billable count overflow")
			}
			totals[key] = p
		}
		for _, p := range totals {
			points = append(points, p)
		}
	}
	return points, nil
}

func depthNumber(row map[string]any, field string) (float64, bool, error) {
	raw, _ := fieldValue(row, field)
	if n, ok := raw.(json.Number); ok {
		value, err := n.Float64()
		if err != nil {
			return 0, false, fmt.Errorf("invalid optional Queue field %s: %w", field, err)
		}
		raw = value
	}
	if raw == nil {
		return 0, false, nil
	}
	if n, ok := nonnegativeNumber(raw); ok {
		return n, true, nil
	}
	if n, ok := raw.(float64); ok && n < 0 && !math.IsInf(n, 0) {
		return 0, false, nil
	}
	return 0, false, fmt.Errorf("invalid optional Queue field %s", field)
}

func (c *groupsCollector) queryDepthRows(ctx context.Context, request cfapi.GraphQLRequest, settings cfapi.DatasetSettings) ([]map[string]any, error) {
	duration := time.Duration(settings.MaxDuration) * time.Second / queueBucketDuration * queueBucketDuration
	if duration < queueBucketDuration {
		return nil, errors.New("queue depth duration limit cannot cover a complete bucket")
	}
	var all []map[string]any
	for start := request.From; start.Before(request.To); {
		end := start.Add(duration)
		if end.After(request.To) {
			end = request.To
		}
		leaf := request
		leaf.From, leaf.To = start, end
		rows, err := c.queryRowsMode(ctx, leaf, true)
		if err != nil {
			return nil, err
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

func (c *groupsCollector) queryDepthLeaf(ctx context.Context, request cfapi.GraphQLRequest) ([]map[string]any, error) {
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

func metricValueKey(point metricValue) string {
	data, _ := json.Marshal([]any{point.name, point.attrs})
	return string(data)
}
