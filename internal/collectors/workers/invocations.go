package workers

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

const invocationsName = "workers.invocations"
const invocationsDataset = "workersInvocationsAdaptive"

type invocationsMetrics struct {
	cfg *config.Config
	api cfapi.Client
}

func (c *invocationsMetrics) Name() string                   { return invocationsName }
func (c *invocationsMetrics) DefaultInterval() time.Duration { return workersBucket }
func (c *invocationsMetrics) Lag() time.Duration             { return 10 * time.Minute }

// LagAt exposes only complete UTC five-minute buckets to the scheduler.
func (c *invocationsMetrics) LagAt(now time.Time) time.Duration {
	return now.Sub(now.UTC().Add(-c.Lag()).Truncate(5 * time.Minute))
}

type invocationValue struct{ field, metric, statistic string }

var invocationSums = []invocationValue{
	{field: "sum.requests", metric: semconv.MetricWorkersInvocations},
	{field: "sum.errors", metric: semconv.MetricWorkersErrors},
	{field: "sum.subrequests", metric: semconv.MetricWorkersSubrequests},
}

func invocationQuantiles() []invocationValue {
	var result []invocationValue
	for _, metric := range []struct{ field, name string }{{"cpuTime", semconv.MetricWorkersCPUTime}, {"wallTime", semconv.MetricWorkersWallTime}, {"requestDuration", semconv.MetricWorkersRequestDuration}} {
		for _, statistic := range []string{"p50", "p75", "p99", "p999"} {
			result = append(result, invocationValue{field: "quantiles." + metric.field + strings.ToUpper(statistic[:1]) + statistic[1:], metric: metric.name, statistic: statistic})
		}
	}
	return result
}

type invocationPoint struct {
	metric, script, status, statistic string
	value                             float64
	at                                time.Time
}

func (c *invocationsMetrics) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if c.cfg == nil || c.cfg.Cloudflare.AccountID == "" || c.api == nil || out == nil {
		return from, errors.New("workers invocations requires config, account, client and emitter")
	}
	if c.cfg.Platform.MaxMetricSeriesPerWindow <= 0 {
		return from, errors.New("workers invocations requires a positive platform series cap")
	}
	start, end := from.UTC().Truncate(workersBucket), to.UTC().Truncate(workersBucket)
	if start.Before(from) {
		start = start.Add(workersBucket)
	}
	if !from.Before(to) || !start.Before(end) {
		return from, errors.New("workers invocations window contains no complete five-minute bucket")
	}
	reader, ok := c.api.(workersSettingsReader)
	if !ok {
		return from, errors.New("client does not expose workers invocations settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, invocationsDataset)
	if err != nil {
		return from, fmt.Errorf("workers invocations settings: %w", err)
	}
	if !settings.Enabled || settings.MaxDuration <= 0 || settings.NotOlderThan <= 0 || settings.MaxPageSize <= 0 {
		return from, errors.New("workers invocations dataset disabled or missing query limits")
	}
	sumFields := []string{"dimensions.scriptName", "dimensions.status", "dimensions.datetimeFiveMinutes"}
	for _, v := range invocationSums {
		sumFields = append(sumFields, v.field)
	}
	for _, f := range sumFields {
		if !workersHasAvailableField(settings.AvailableFields, f) {
			return from, fmt.Errorf("workers invocations missing required field %s", f)
		}
	}
	fieldLimit := settings.MaxNumberOfFields
	if fieldLimit > 35 {
		fieldLimit = 35
	}
	if len(sumFields) > fieldLimit {
		return from, errors.New("workers invocations required sums exceed field limit")
	}
	limit := settings.MaxPageSize
	if limit > workersQueryLimit {
		limit = workersQueryLimit
	}
	request := cfapi.GraphQLRequest{Scope: cfapi.AccountScope, ScopeID: c.cfg.Cloudflare.AccountID, Dataset: invocationsDataset, WantedFields: sumFields, Limit: limit}
	rows, err := c.queryInvocations(ctx, request, start, end)
	if err != nil {
		return from, err
	}
	points := map[string]invocationPoint{}
	for _, row := range rows {
		bucket, e := workersRowBucket(row)
		if e != nil {
			return from, e
		}
		if !workersCompleteBucket(bucket, start, end) {
			continue
		}
		dimensions := row["dimensions"].(map[string]any)
		status, ok := dimensions["status"].(string)
		if !ok || status == "" {
			return from, errors.New("workers invocations row has no status")
		}
		script := workersBoundedName(row)
		for _, v := range invocationSums {
			value, ok := invocationNumber(row, v.field, true)
			if !ok {
				return from, fmt.Errorf("workers invocations invalid %s", v.field)
			}
			p := invocationPoint{metric: v.metric, script: script, value: value}
			if v.metric == semconv.MetricWorkersInvocations {
				p.status = status
			}
			key := invocationPointKey(p)
			previous := points[key]
			p.value += previous.value
			if math.IsInf(p.value, 0) {
				return from, errors.New("workers invocation counter overflow")
			}
			points[key] = p
		}
	}
	// Quantiles cannot be combined across invocation statuses. Query them separately
	// at script/bucket granularity and retain the latest complete bucket per script.
	// Never average percentiles or let an arbitrary status overwrite another.
	quantileFields := []string{"dimensions.scriptName", "dimensions.datetimeFiveMinutes"}
	var quantiles []invocationValue
	for _, v := range invocationQuantiles() {
		if workersHasAvailableField(settings.AvailableFields, v.field) {
			quantiles = append(quantiles, v)
			quantileFields = append(quantileFields, v.field)
		}
	}
	if len(quantiles) > 0 {
		if len(quantileFields) > fieldLimit {
			return from, errors.New("workers invocations entitled quantiles exceed field limit")
		}
		request.WantedFields = quantileFields
		rows, err = c.queryInvocations(ctx, request, start, end)
		if err != nil {
			return from, err
		}
		for _, row := range rows {
			bucket, e := workersRowBucket(row)
			if e != nil {
				return from, e
			}
			if !workersCompleteBucket(bucket, start, end) {
				continue
			}
			script := workersBoundedName(row)
			// Invalid names collapse for counters, but must not merge unrelated quantiles.
			if script == "" {
				continue
			}
			for _, v := range quantiles {
				value, ok := invocationNumber(row, v.field, false)
				if !ok {
					return from, fmt.Errorf("workers invocations invalid %s", v.field)
				}
				p := invocationPoint{metric: v.metric, script: script, statistic: v.statistic, value: value / 1e6, at: bucket}
				key := invocationPointKey(p)
				if previous, exists := points[key]; exists {
					if previous.at.Equal(bucket) {
						return from, errors.New("workers invocations duplicate script quantile bucket")
					}
					if previous.at.After(bucket) {
						continue
					}
				}
				points[key] = p
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}
	keys := make([]string, 0, len(points))
	for key := range points {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if cap := c.cfg.Platform.MaxMetricSeriesPerWindow; len(keys) > cap {
		slog.WarnContext(ctx, "platform metric series dropped", "collector", invocationsName, "dropped_series", len(keys)-cap)
		keys = keys[:cap]
	}
	for _, key := range keys {
		p := points[key]
		var attrs []telemetry.Attr
		if p.script != "" {
			attrs = append(attrs, telemetry.Attr{Key: semconv.AttrWorkersScriptName, Value: p.script})
		}
		if p.status != "" {
			attrs = append(attrs, telemetry.Attr{Key: semconv.AttrWorkersStatus, Value: p.status})
		}
		if p.statistic != "" {
			attrs = append(attrs, telemetry.Attr{Key: semconv.AttrStatistic, Value: p.statistic})
			err = out.Gauge(ctx, p.metric, p.value, attrs...)
		} else {
			err = out.Counter(ctx, p.metric, p.value, attrs...)
		}
		if err != nil {
			return from, err
		}
	}
	return end, nil
}
func invocationPointKey(p invocationPoint) string {
	// JSON gives an unambiguous key even when source strings contain separators.
	encoded, _ := json.Marshal([]string{p.metric, p.script, p.status, p.statistic})
	return string(encoded)
}
func invocationNumber(row map[string]any, field string, integer bool) (float64, bool) {
	parts := strings.SplitN(field, ".", 2)
	values, ok := row[parts[0]].(map[string]any)
	if !ok {
		return 0, false
	}
	if integer {
		return workersCount(values[parts[1]])
	}
	var value float64
	switch v := values[parts[1]].(type) {
	case float64:
		value = v
	case float32:
		value = float64(v)
	case int:
		value = float64(v)
	case json.Number:
		n, e := v.Float64()
		if e != nil {
			return 0, false
		}
		value = n
	case string:
		n, e := strconv.ParseFloat(v, 64)
		if e != nil {
			return 0, false
		}
		value = n
	default:
		return 0, false
	}
	return value, value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
func (c *invocationsMetrics) queryInvocations(ctx context.Context, request cfapi.GraphQLRequest, from, to time.Time) ([]map[string]any, error) {
	return collector.Bisect(from, to, workersBucket, workersBucket, func(start, end time.Time) ([]map[string]any, bool, error) {
		leaf := request
		leaf.From = start
		leaf.To = end
		var rows []map[string]any
		err := c.api.Query(ctx, leaf, &rows)
		sat, ok := cfapi.AsSaturation(err)
		saturated := (ok && sat.Dataset == invocationsDataset) || (err == nil && len(rows) >= request.Limit)
		if saturated && err == nil {
			err = fmt.Errorf("workers invocations reached query limit %d", request.Limit)
		}
		return rows, saturated, err
	})
}

var _ collector.WindowCollector = (*invocationsMetrics)(nil)
