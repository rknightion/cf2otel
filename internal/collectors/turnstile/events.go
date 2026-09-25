package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	turnstileCollectorName = "turnstile.events"
	turnstileDataset       = "turnstileAdaptiveGroups"
	turnstileQueryLimit    = 10000
	turnstileBucket        = 5 * time.Minute
)

var turnstileFields = []string{"count", "dimensions.datetimeFiveMinutes"}

type turnstileSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type eventMetrics struct {
	cfg *config.Config
	api cfapi.Client
}

func NewEventMetrics(cfg *config.Config, api cfapi.Client) *eventMetrics {
	return &eventMetrics{cfg: cfg, api: api}
}

func (*eventMetrics) Name() string                   { return turnstileCollectorName }
func (*eventMetrics) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*eventMetrics) Lag() time.Duration             { return 10 * time.Minute }

func (c *eventMetrics) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid Turnstile events window")
	}
	windowTo := to.UTC().Truncate(turnstileBucket)
	if !windowTo.After(from) {
		return from, errors.New("turnstile window contains no complete five-minute bucket")
	}
	windowFrom := from.UTC().Truncate(turnstileBucket)
	if windowFrom.Before(from) {
		windowFrom = windowFrom.Add(turnstileBucket)
	}
	if !windowFrom.Before(windowTo) {
		return windowTo, nil
	}
	if c.cfg == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("turnstile events requires a configured Cloudflare account")
	}
	if c.api == nil {
		return from, errors.New("turnstile events requires a Cloudflare client")
	}
	seriesCap := c.cfg.Platform.MaxMetricSeriesPerWindow
	if seriesCap <= 0 {
		return from, errors.New("turnstile events has an invalid platform metric series limit")
	}
	reader, ok := c.api.(turnstileSettingsReader)
	if !ok {
		return from, errors.New("cloudflare client does not expose Turnstile dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, turnstileDataset)
	if err != nil {
		return from, fmt.Errorf("read Turnstile dataset settings: %w", err)
	}
	if !settings.Enabled {
		return from, errors.New("turnstile Groups dataset is disabled")
	}
	if settings.MaxDuration <= 0 || settings.NotOlderThan <= 0 {
		return from, errors.New("turnstile dataset is missing its duration or retention limit")
	}
	if settings.MaxPageSize <= 0 {
		return from, errors.New("turnstile dataset is missing its page-size limit")
	}
	if settings.MaxNumberOfFields < len(turnstileFields) {
		return from, errors.New("turnstile dataset field limit is below the required field count")
	}
	for _, field := range turnstileFields {
		if !turnstileHasAvailableField(settings.AvailableFields, field) {
			return from, fmt.Errorf("turnstile dataset is missing required field %s", field)
		}
	}
	limit := turnstileQueryLimit
	if settings.MaxPageSize < limit {
		limit = settings.MaxPageSize
	}
	request := cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      turnstileDataset,
		WantedFields: append([]string(nil), turnstileFields...),
		Limit:        limit,
	}
	rows, err := c.queryWindow(ctx, request, windowFrom, windowTo, limit)
	if err != nil {
		return from, fmt.Errorf("query Turnstile dataset: %w", err)
	}

	var total float64
	hasCompleteBucket := false
	for _, row := range rows {
		bucket, err := turnstileRowBucket(row)
		if err != nil {
			return from, err
		}
		if !turnstileCompleteBucket(bucket, from, windowTo) {
			continue
		}
		count, ok := turnstileCount(row["count"])
		if !ok {
			return from, errors.New("turnstile row has an invalid count")
		}
		total += count
		hasCompleteBucket = true
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}
	if hasCompleteBucket {
		if err := out.Counter(ctx, semconv.MetricTurnstileEvents, total); err != nil {
			return from, err
		}
	}
	return windowTo, nil
}

func (c *eventMetrics) queryWindow(ctx context.Context, request cfapi.GraphQLRequest, from, to time.Time, limit int) ([]map[string]any, error) {
	request.From = from
	request.To = to
	request.Limit = limit
	var rows []map[string]any
	err := c.api.Query(ctx, request, &rows)
	saturated := turnstileIsSaturation(err) || (err == nil && len(rows) >= limit)
	if !saturated {
		if err != nil {
			return nil, err
		}
		return rows, nil
	}
	if to.Sub(from) <= turnstileBucket {
		if err == nil {
			err = fmt.Errorf("GraphQL dataset %s reached requested limit %d", turnstileDataset, limit)
		}
		return nil, fmt.Errorf("turnstile query still saturates a five-minute bucket: %w", err)
	}
	mid := from.Add(to.Sub(from) / 2).Truncate(turnstileBucket)
	if !from.Before(mid) || !mid.Before(to) {
		return nil, fmt.Errorf("cannot split saturated Turnstile window %s..%s", from.Format(time.RFC3339), to.Format(time.RFC3339))
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

func turnstileIsSaturation(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "GraphQL dataset "+turnstileDataset+" window ") && strings.Contains(message, " saturated limit ")
}

func turnstileHasAvailableField(available []string, wanted string) bool {
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

func turnstileRowBucket(row map[string]any) (time.Time, error) {
	dimensions, ok := row["dimensions"].(map[string]any)
	if !ok {
		return time.Time{}, errors.New("turnstile row is missing dimensions")
	}
	raw, ok := dimensions["datetimeFiveMinutes"].(string)
	if !ok {
		return time.Time{}, errors.New("turnstile row is missing its five-minute timestamp")
	}
	bucket, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("turnstile row has an invalid five-minute timestamp")
	}
	bucket = bucket.UTC()
	if !bucket.Equal(bucket.Truncate(turnstileBucket)) {
		return time.Time{}, errors.New("turnstile row timestamp is not a five-minute bucket")
	}
	return bucket, nil
}

func turnstileCompleteBucket(bucket, from, to time.Time) bool {
	return !bucket.Before(from) && !bucket.Add(turnstileBucket).After(to)
}

func turnstileCount(value any) (float64, bool) {
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

var _ collector.WindowCollector = (*eventMetrics)(nil)
