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
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const (
	workersCollectorName = "workers.overview"
	workersDataset       = "workersOverviewRequestsAdaptiveGroups"
	workersQueryLimit    = 10000
	workersBucket        = 5 * time.Minute
)

var workersRequiredFields = []string{"count", "dimensions.datetimeFiveMinutes"}

type workersSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type overviewMetrics struct {
	cfg *config.Config
	api cfapi.Client
}

func NewOverviewMetrics(cfg *config.Config, api cfapi.Client) *overviewMetrics {
	return &overviewMetrics{cfg: cfg, api: api}
}

func (*overviewMetrics) Name() string                   { return workersCollectorName }
func (*overviewMetrics) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*overviewMetrics) Lag() time.Duration             { return 10 * time.Minute }

func (c *overviewMetrics) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid Workers overview window")
	}
	windowTo := to.UTC().Truncate(workersBucket)
	if !windowTo.After(from) {
		return from, errors.New("workers overview window contains no complete five-minute bucket")
	}
	windowFrom := from.UTC().Truncate(workersBucket)
	if windowFrom.Before(from) {
		windowFrom = windowFrom.Add(workersBucket)
	}
	if !windowFrom.Before(windowTo) {
		return windowTo, nil
	}
	if c.cfg == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("workers overview requires a configured Cloudflare account")
	}
	if c.api == nil {
		return from, errors.New("workers overview requires a Cloudflare client")
	}
	seriesCap := c.cfg.Platform.MaxMetricSeriesPerWindow
	if seriesCap <= 0 {
		return from, errors.New("workers overview has an invalid platform metric series limit")
	}
	reader, ok := c.api.(workersSettingsReader)
	if !ok {
		return from, errors.New("cloudflare client does not expose Workers overview dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, workersDataset)
	if err != nil {
		return from, fmt.Errorf("read Workers overview dataset settings: %w", err)
	}
	if !settings.Enabled {
		return from, errors.New("workers overview dataset is disabled")
	}
	if settings.MaxDuration <= 0 || settings.NotOlderThan <= 0 {
		return from, errors.New("workers overview dataset is missing its duration or retention limit")
	}
	if settings.MaxPageSize <= 0 {
		return from, errors.New("workers overview dataset is missing its page-size limit")
	}
	if settings.MaxNumberOfFields < len(workersRequiredFields) {
		return from, errors.New("workers overview dataset field limit is below the required field count")
	}
	for _, field := range workersRequiredFields {
		if !workersHasAvailableField(settings.AvailableFields, field) {
			return from, fmt.Errorf("workers overview dataset is missing required field %s", field)
		}
	}

	wantedFields := append([]string(nil), workersRequiredFields...)
	includeName := settings.MaxNumberOfFields > len(wantedFields) && workersHasAvailableField(settings.AvailableFields, "dimensions.scriptName")
	if includeName {
		wantedFields = append(wantedFields, "dimensions.scriptName")
	}
	limit := workersQueryLimit
	if settings.MaxPageSize < limit {
		limit = settings.MaxPageSize
	}
	request := cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      workersDataset,
		WantedFields: wantedFields,
		Limit:        limit,
	}
	rows, err := c.queryWindow(ctx, request, windowFrom, windowTo, limit)
	if err != nil {
		return from, fmt.Errorf("query Workers overview dataset: %w", err)
	}

	series := make(map[string]float64)
	for _, row := range rows {
		bucket, err := workersRowBucket(row)
		if err != nil {
			return from, err
		}
		if !workersCompleteBucket(bucket, from, windowTo) {
			continue
		}
		count, ok := workersCount(row["count"])
		if !ok {
			return from, errors.New("workers overview row has an invalid count")
		}
		name := ""
		if includeName {
			name = workersBoundedName(row)
		}
		series[name] += count
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}

	names := make([]string, 0, len(series))
	for name := range series {
		names = append(names, name)
	}
	sort.Strings(names)
	dropped := 0
	if len(names) > seriesCap {
		dropped = len(names) - seriesCap
		names = names[:seriesCap]
		slog.WarnContext(ctx, "platform metric series dropped", "collector", workersCollectorName, "dropped_series", dropped)
	}
	for _, name := range names {
		var attrs []telemetry.Attr
		if name != "" {
			attrs = []telemetry.Attr{{Key: semconv.AttrWorkersScriptName, Value: name}}
		}
		if err := out.Counter(ctx, semconv.MetricWorkersRequests, series[name], attrs...); err != nil {
			return from, err
		}
	}
	return windowTo, nil
}

func (c *overviewMetrics) queryWindow(ctx context.Context, request cfapi.GraphQLRequest, from, to time.Time, limit int) ([]map[string]any, error) {
	request.From = from
	request.To = to
	request.Limit = limit
	var rows []map[string]any
	err := c.api.Query(ctx, request, &rows)
	saturated := workersIsSaturation(err) || (err == nil && len(rows) >= limit)
	if !saturated {
		if err != nil {
			return nil, err
		}
		return rows, nil
	}
	if to.Sub(from) <= workersBucket {
		if err == nil {
			err = fmt.Errorf("GraphQL dataset %s reached requested limit %d", workersDataset, limit)
		}
		return nil, fmt.Errorf("workers overview query still saturates a five-minute bucket: %w", err)
	}
	mid := from.Add(to.Sub(from) / 2).Truncate(workersBucket)
	if !from.Before(mid) || !mid.Before(to) {
		return nil, fmt.Errorf("cannot split saturated Workers overview window %s..%s", from.Format(time.RFC3339), to.Format(time.RFC3339))
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

func workersIsSaturation(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "GraphQL dataset "+workersDataset+" window ") && strings.Contains(message, " saturated limit ")
}

func workersHasAvailableField(available []string, wanted string) bool {
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

func workersRowBucket(row map[string]any) (time.Time, error) {
	dimensions, ok := row["dimensions"].(map[string]any)
	if !ok {
		return time.Time{}, errors.New("workers overview row is missing dimensions")
	}
	raw, ok := dimensions["datetimeFiveMinutes"].(string)
	if !ok {
		return time.Time{}, errors.New("workers overview row is missing its five-minute timestamp")
	}
	bucket, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("workers overview row has an invalid five-minute timestamp")
	}
	bucket = bucket.UTC()
	if !bucket.Equal(bucket.Truncate(workersBucket)) {
		return time.Time{}, errors.New("workers overview row timestamp is not a five-minute bucket")
	}
	return bucket, nil
}

func workersCompleteBucket(bucket, from, to time.Time) bool {
	return !bucket.Before(from) && !bucket.Add(workersBucket).After(to)
}

func workersCount(value any) (float64, bool) {
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

func workersBoundedName(row map[string]any) string {
	dimensions, ok := row["dimensions"].(map[string]any)
	if !ok {
		return ""
	}
	name, ok := dimensions["scriptName"].(string)
	if !ok {
		return ""
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		return ""
	}
	return name
}

var _ collector.WindowCollector = (*overviewMetrics)(nil)
