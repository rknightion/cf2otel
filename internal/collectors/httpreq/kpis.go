package httpreq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// threats uses the hourly rollup's source policy, not an invented eyeball filter.
// Free and Pro advertise this dataset (observed retention ~3d and ~7d);
// discovery, rather than those observations, controls each query.
type threats struct{ base }

func (*threats) Name() string                   { return "httpreq.threats" }
func (*threats) DefaultInterval() time.Duration { return time.Hour }
func (*threats) Lag() time.Duration             { return 10 * time.Minute }
func (*threats) LagAt(now time.Time) time.Duration {
	return now.Sub(now.UTC().Add(-10 * time.Minute).Truncate(time.Hour))
}

type transfer struct {
	base
	now func() time.Time
}

func (*transfer) Name() string                   { return "httpreq.transfer" }
func (*transfer) DefaultInterval() time.Duration { return time.Hour }

// kpiSettings requires every selected leaf. Dropping a required leaf would turn
// an unentitled sum into a plausible but incorrect zero.
func (b base) kpiSettings(ctx context.Context, scope cfapi.Scope, id, dataset string, fields []string) (cfapi.DatasetSettings, error) {
	provider, ok := b.api.(httpGroupSettingsProvider)
	if !ok {
		return cfapi.DatasetSettings{}, errors.New("HTTP KPI settings discovery is unavailable")
	}
	s, err := provider.DatasetSettings(ctx, scope, id, dataset)
	if err != nil {
		return s, err
	}
	if !s.Enabled {
		return s, errors.New("HTTP KPI dataset is disabled")
	}
	available := map[string]bool{}
	for _, f := range s.AvailableFields {
		available[httpGroupFieldName(f)] = true
	}
	for _, f := range fields {
		if !available[httpGroupFieldName(f)] {
			return s, fmt.Errorf("HTTP KPI missing required field %s", f)
		}
	}
	if s.MaxNumberOfFields > 0 && len(fields) > s.MaxNumberOfFields {
		return s, errors.New("HTTP KPI field budget is insufficient")
	}
	return s, nil
}

// QueryBatch preserves the raw dataset: cfapi.Query normalizes null to [],
// which cannot prove a successful empty sum. A single strict selection also
// prevents required-field renegotiation from silently dropping a leaf.
func (b base) kpiRows(ctx context.Context, req cfapi.GraphQLRequest, s cfapi.DatasetSettings, now time.Time) ([]map[string]any, error) {
	if s.NotOlderThan > 0 && req.From.Before(now.Add(-time.Duration(s.NotOlderThan)*time.Second)) {
		return nil, &cfapi.RetentionGapError{Dataset: req.Dataset, Floor: now.Add(-time.Duration(s.NotOlderThan) * time.Second)}
	}
	if s.MaxDuration > 0 && req.To.Sub(req.From) > time.Duration(s.MaxDuration)*time.Second {
		return nil, errors.New("HTTP KPI window exceeds dataset duration")
	}
	batch, ok := b.api.(cfapi.GraphQLBatchQuerier)
	if !ok {
		return nil, errors.New("HTTP KPIs require strict batch support")
	}
	raw, err := batch.QueryBatch(ctx, []cfapi.GraphQLBatchSelection{{Alias: "kpi", Request: req}})
	if err != nil {
		return nil, err
	}
	value := raw["kpi"]
	var rows []map[string]any
	if err = json.Unmarshal(value, &rows); err != nil {
		return nil, errors.New("HTTP KPI dataset is not an array")
	}
	if rows == nil {
		return nil, errors.New("HTTP KPI dataset is null")
	}
	limit := req.Limit
	if s.MaxPageSize > 0 && s.MaxPageSize < limit {
		limit = s.MaxPageSize
	}
	if len(rows) >= limit {
		return nil, errors.New("HTTP KPI query reached the row limit")
	}
	return rows, nil
}
func addKPISum(rows []map[string]any, key string) (float64, error) {
	total := float64(0)
	for _, row := range rows {
		sum, _ := row["sum"].(map[string]any)
		v, ok := metricNumber(sum[key])
		if !ok || v < 0 || math.IsInf(total+v, 0) {
			return 0, fmt.Errorf("HTTP KPI invalid %s sum", key)
		}
		total += v
	}
	return total, nil
}
func (c *threats) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	// Advance a legacy/fractional cursor past its incomplete first hour, but
	// never query or emit that partial rollup. Persist only complete-hour ends.
	start := from.UTC().Truncate(time.Hour)
	if start.Before(from) {
		start = start.Add(time.Hour)
	}
	end := to.UTC().Truncate(time.Hour)
	if !start.Before(end) {
		if start.After(from) && !start.After(to) {
			return start, nil
		}
		return from, nil
	}
	zones, err := c.metricZones(ctx)
	if err != nil {
		return from, err
	}
	if len(zones) > c.cfg.HTTP.MaxMetricSeriesPerWindow {
		return from, errors.New("HTTP threats exceed metric series limit")
	}
	totals := make([]float64, len(zones))
	now := time.Now().UTC()
	fields := []string{"sum.threats", "dimensions.datetime"}
	for i, z := range zones {
		s, err := c.kpiSettings(ctx, cfapi.ZoneScope, z.ID, "httpRequests1hGroups", fields)
		if err != nil {
			return from, err
		}
		rows, err := c.kpiRows(ctx, cfapi.GraphQLRequest{Scope: cfapi.ZoneScope, ScopeID: z.ID, Dataset: "httpRequests1hGroups", WantedFields: fields, From: start, To: end, Limit: 10000}, s, now)
		if err != nil {
			return from, err
		}
		seen := map[time.Time]bool{}
		for _, row := range rows {
			dims, _ := row["dimensions"].(map[string]any)
			at, err := time.Parse(time.RFC3339, str(dims["datetime"]))
			if err != nil || !at.Equal(at.Truncate(time.Hour)) || at.Before(start) || !at.Before(end) || seen[at] {
				return from, errors.New("HTTP threats invalid or duplicate hourly bucket")
			}
			seen[at] = true
		}
		totals[i], err = addKPISum(rows, "threats")
		if err != nil {
			return from, err
		}
	}
	for i, z := range zones {
		if err := e.Counter(ctx, semconv.MetricHTTPThreats, totals[i], telemetry.Attr{Key: semconv.AttrHTTPZone, Value: z.Name}); err != nil {
			return from, err
		}
	}
	return end, nil
}
func (c *transfer) Collect(ctx context.Context, e telemetry.Emitter) error {
	now := c.now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := now.Add(-5 * time.Minute).Truncate(5 * time.Minute)
	bytes := float64(0)
	projected := float64(0)
	// A holdback crossing the month boundary is a zero-length CURRENT month,
	// never a fetch of the previous month.
	if end.After(start) {
		if c.cfg.Cloudflare.AccountID == "" {
			return errors.New("HTTP account transfer requires an account ID")
		}
		fields := []string{"sum.edgeResponseBytes"}
		s, err := c.kpiSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, "httpRequestsAdaptiveGroups", fields)
		if err != nil {
			return err
		}
		if s.NotOlderThan > 0 && start.Before(now.Add(-time.Duration(s.NotOlderThan)*time.Second)) {
			return &cfapi.RetentionGapError{Dataset: "httpRequestsAdaptiveGroups", Floor: now.Add(-time.Duration(s.NotOlderThan) * time.Second)}
		}
		for from := start; from.Before(end); {
			to := end
			if s.MaxDuration > 0 && from.Add(time.Duration(s.MaxDuration)*time.Second).Before(to) {
				to = from.Add(time.Duration(s.MaxDuration) * time.Second)
			}
			rows, err := c.kpiRows(ctx, cfapi.GraphQLRequest{Scope: cfapi.AccountScope, ScopeID: c.cfg.Cloudflare.AccountID, Dataset: "httpRequestsAdaptiveGroups", WantedFields: fields, From: from, To: to, Filter: map[string]any{"requestSource": "eyeball"}, Limit: 10000}, s, now)
			if err != nil {
				return err
			}
			value, err := addKPISum(rows, "edgeResponseBytes")
			if err != nil {
				return err
			}
			bytes += value
			if math.IsInf(bytes, 0) {
				return errors.New("HTTP account transfer sum overflow")
			}
			from = to
		}
		monthSeconds := start.AddDate(0, 1, 0).Sub(start).Seconds()
		projected = bytes * (monthSeconds / end.Sub(start).Seconds())
		if math.IsInf(projected, 0) {
			return errors.New("HTTP account transfer projection overflow")
		}
	}
	if err := e.Gauge(ctx, semconv.MetricHTTPAccountTransferMTD, bytes); err != nil {
		return err
	}
	return e.Gauge(ctx, semconv.MetricHTTPAccountTransferProjection, projected)
}
