package rum

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const (
	pageloadDataset        = "rumPageloadEventsAdaptiveGroups"
	webVitalsDataset       = "rumWebVitalsEventsAdaptiveGroups"
	queryLimit             = 10000
	webVitalsRollingWindow = time.Hour
)

const rumLag = 10 * time.Minute

var pageloadRequiredFields = []string{
	"count",
	"sum.visits",
	"dimensions.countryName",
	"dimensions.deviceType",
}

var pageloadOptionalFields = []string{
	"dimensions.siteTag",
}

// Cloudflare's Groups timing quantiles are normalized from microseconds to the
// Web Analytics millisecond convention. CLS is a unitless score.
var webVitalsQuantiles = []struct {
	field   string
	metric  string
	divisor float64
}{
	{field: "largestContentfulPaintP75", metric: semconv.MetricRUMLCPP75, divisor: 1000},
	{field: "interactionToNextPaintP75", metric: semconv.MetricRUMINPP75, divisor: 1000},
	{field: "firstInputDelayP75", metric: semconv.MetricRUMFIDP75, divisor: 1000},
	{field: "firstContentfulPaintP75", metric: semconv.MetricRUMFCPP75, divisor: 1000},
	{field: "timeToFirstByteP75", metric: semconv.MetricRUMTTFBP75, divisor: 1000},
	{field: "cumulativeLayoutShiftP75", metric: semconv.MetricRUMCLSP75, divisor: 1},
}

type datasetSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type base struct {
	cfg *config.Config
	api cfapi.Client
}

type pageloads struct{ base }

type webVitals struct {
	base
	mu        sync.Mutex
	previous  map[vitalSeries]struct{}
	pending   map[vitalSeries]struct{}
	pendingTo time.Time
}

type pageloadSample struct {
	pageViews float64
	sessions  float64
	attrs     []telemetry.Attr
}

type vitalSeries struct {
	metric     string
	device     string
	siteTag    string
	hasSiteTag bool
}

func NewPageloads(cfg *config.Config, api cfapi.Client) *pageloads {
	return &pageloads{base{cfg: cfg, api: api}}
}

func NewWebVitals(cfg *config.Config, api cfapi.Client) *webVitals {
	return &webVitals{base: base{cfg: cfg, api: api}, previous: map[vitalSeries]struct{}{}}
}

func (*pageloads) Name() string                   { return "rum.pageloads" }
func (*webVitals) Name() string                   { return "rum.web_vitals" }
func (*pageloads) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*webVitals) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*pageloads) Lag() time.Duration             { return rumLag }
func (*webVitals) Lag() time.Duration             { return rumLag }

func (c *pageloads) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid RUM pageload window")
	}

	settings, err := c.settings(ctx, pageloadDataset)
	if err != nil {
		return from, err
	}
	wanted, err := fieldsForAccount(settings, pageloadRequiredFields, pageloadOptionalFields)
	if err != nil {
		return from, fmt.Errorf("RUM pageload Groups: %w", err)
	}
	var rows []map[string]any
	if err := c.api.Query(ctx, cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      pageloadDataset,
		WantedFields: wanted,
		From:         from,
		To:           to,
		Limit:        queryLimit,
	}, &rows); err != nil {
		return from, fmt.Errorf("RUM pageload Groups: %w", err)
	}

	samples := make([]pageloadSample, 0, len(rows))
	for _, row := range rows {
		pageViews, ok := numericValue(row["count"])
		if !ok || !nonnegativeFinite(pageViews) {
			return from, errors.New("RUM pageload Groups row is missing a valid nonnegative count")
		}
		sum, _ := row["sum"].(map[string]any)
		sessions, ok := numericValue(sum["visits"])
		if !ok || !nonnegativeFinite(sessions) {
			return from, errors.New("RUM pageload Groups row is missing a valid nonnegative visits sum")
		}
		dimensions, ok := row["dimensions"].(map[string]any)
		if !ok || dimensions == nil {
			return from, errors.New("RUM pageload Groups row is missing dimensions")
		}
		country, ok := dimensions["countryName"]
		if !ok {
			return from, errors.New("RUM pageload Groups row is missing countryName dimension")
		}
		device, ok := dimensions["deviceType"]
		if !ok {
			return from, errors.New("RUM pageload Groups row is missing deviceType dimension")
		}
		attrs := make([]telemetry.Attr, 0, 3)
		if contains(wanted, "dimensions.siteTag") {
			if siteTag, ok := dimensions["siteTag"]; ok && siteTag != nil {
				attrs = append(attrs, telemetry.Attr{Key: semconv.AttrRUMSiteTag, Value: fmt.Sprint(siteTag)})
			}
		}
		attrs = append(attrs,
			telemetry.Attr{Key: semconv.AttrRUMCountry, Value: dimensionValue(country)},
			telemetry.Attr{Key: semconv.AttrRUMDeviceType, Value: dimensionValue(device)},
		)
		samples = append(samples, pageloadSample{pageViews: pageViews, sessions: sessions, attrs: attrs})
	}

	for _, sample := range samples {
		if err := out.Counter(ctx, semconv.MetricRUMPageViews, sample.pageViews, sample.attrs...); err != nil {
			return from, err
		}
		if err := out.Counter(ctx, semconv.MetricRUMSessions, sample.sessions, sample.attrs...); err != nil {
			return from, err
		}
	}
	return to, nil
}

func (c *webVitals) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid RUM web-vitals window")
	}

	settings, err := c.settings(ctx, webVitalsDataset)
	if err != nil {
		return from, err
	}
	required := make([]string, 0, len(webVitalsQuantiles)+1)
	required = append(required, "dimensions.deviceType")
	for _, quantile := range webVitalsQuantiles {
		required = append(required, "quantiles."+quantile.field)
	}
	wanted, err := fieldsForAccount(settings, required, []string{"dimensions.siteTag"})
	if err != nil {
		return from, fmt.Errorf("RUM web-vitals Groups: %w", err)
	}

	// Requery a bounded rolling interval so late-arriving values replace prior
	// gauge values. The scheduler still advances its checkpoint through [from,to).
	queryFrom := to.Add(-webVitalsRollingWindow)
	if !queryFrom.Before(to) {
		return from, errors.New("invalid RUM web-vitals rolling window")
	}
	var rows []map[string]any
	if err := c.api.Query(ctx, cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      webVitalsDataset,
		WantedFields: wanted,
		From:         queryFrom,
		To:           to,
		Limit:        queryLimit,
	}, &rows); err != nil {
		return from, fmt.Errorf("RUM web-vitals Groups: %w", err)
	}

	current := make(map[vitalSeries]float64)
	for _, row := range rows {
		dimensions, ok := row["dimensions"].(map[string]any)
		if !ok || dimensions == nil {
			return from, errors.New("RUM web-vitals Groups row is missing dimensions")
		}
		device, ok := dimensions["deviceType"]
		if !ok {
			return from, errors.New("RUM web-vitals Groups row is missing deviceType dimension")
		}
		seriesBase := vitalSeries{device: dimensionValue(device)}
		if contains(wanted, "dimensions.siteTag") {
			if siteTag, ok := dimensions["siteTag"]; ok && siteTag != nil {
				seriesBase.siteTag = fmt.Sprint(siteTag)
				seriesBase.hasSiteTag = true
			}
		}
		quantiles, ok := row["quantiles"].(map[string]any)
		if !ok || quantiles == nil {
			return from, errors.New("RUM web-vitals Groups row is missing quantiles")
		}
		for _, quantile := range webVitalsQuantiles {
			raw, ok := quantiles[quantile.field]
			if !ok {
				return from, fmt.Errorf("RUM web-vitals Groups row is missing %s", quantile.field)
			}
			if raw == nil {
				continue
			}
			value, ok := numericValue(raw)
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
				return from, fmt.Errorf("RUM web-vitals Groups row has invalid %s", quantile.field)
			}
			if value == -1 {
				continue
			}
			if value < 0 {
				return from, fmt.Errorf("RUM web-vitals Groups row has negative %s", quantile.field)
			}
			value /= quantile.divisor
			series := seriesBase
			series.metric = quantile.metric
			if _, duplicate := current[series]; duplicate {
				return from, fmt.Errorf("RUM web-vitals Groups returned duplicate %s device series", quantile.metric)
			}
			current[series] = value
		}
	}

	if err := c.emitReplacing(ctx, out, from, to, current); err != nil {
		return from, err
	}
	return to, nil
}

func (c *webVitals) emitReplacing(ctx context.Context, out telemetry.Emitter, from, to time.Time, current map[vitalSeries]float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A later window is only scheduled after the preceding buffer was flushed
	// and checkpointed. A retry of the same window must repeat stale zeros.
	if !c.pendingTo.IsZero() && !from.Before(c.pendingTo) {
		c.previous = c.pending
		c.pending = nil
		c.pendingTo = time.Time{}
	}

	stale := make([]vitalSeries, 0)
	for series := range c.previous {
		if _, ok := current[series]; !ok {
			stale = append(stale, series)
		}
	}
	sortVitalSeries(stale)
	for _, series := range stale {
		if err := out.Gauge(ctx, series.metric, 0, series.attrs()...); err != nil {
			return err
		}
	}
	active := make([]vitalSeries, 0, len(current))
	for series := range current {
		active = append(active, series)
	}
	sortVitalSeries(active)
	for _, series := range active {
		if err := out.Gauge(ctx, series.metric, current[series], series.attrs()...); err != nil {
			return err
		}
	}
	updated := make(map[vitalSeries]struct{}, len(current))
	for series := range current {
		updated[series] = struct{}{}
	}
	c.pending = updated
	c.pendingTo = to
	return nil
}

func (s vitalSeries) attrs() []telemetry.Attr {
	attrs := make([]telemetry.Attr, 0, 2)
	if s.hasSiteTag {
		attrs = append(attrs, telemetry.Attr{Key: semconv.AttrRUMSiteTag, Value: s.siteTag})
	}
	return append(attrs, telemetry.Attr{Key: semconv.AttrRUMDeviceType, Value: s.device})
}

func sortVitalSeries(series []vitalSeries) {
	sort.Slice(series, func(i, j int) bool {
		if series[i].metric != series[j].metric {
			return series[i].metric < series[j].metric
		}
		if series[i].device != series[j].device {
			return series[i].device < series[j].device
		}
		if series[i].hasSiteTag != series[j].hasSiteTag {
			return !series[i].hasSiteTag
		}
		return series[i].siteTag < series[j].siteTag
	})
}

func (b base) settings(ctx context.Context, dataset string) (cfapi.DatasetSettings, error) {
	if b.cfg == nil || b.cfg.Cloudflare.AccountID == "" {
		return cfapi.DatasetSettings{}, errors.New("Cloudflare account ID is required for RUM")
	}
	reader, ok := b.api.(datasetSettingsReader)
	if !ok {
		return cfapi.DatasetSettings{}, errors.New("Cloudflare client does not expose account dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, b.cfg.Cloudflare.AccountID, dataset)
	if err != nil {
		return cfapi.DatasetSettings{}, fmt.Errorf("read RUM %s settings: %w", dataset, err)
	}
	return settings, nil
}

func fieldsForAccount(settings cfapi.DatasetSettings, required, optional []string) ([]string, error) {
	if !settings.Enabled {
		return nil, errors.New("dataset disabled")
	}
	if settings.MaxNumberOfFields > 0 && len(required) > settings.MaxNumberOfFields {
		return nil, fmt.Errorf("required selection needs %d fields, dataset limit is %d", len(required), settings.MaxNumberOfFields)
	}
	selected := make([]string, 0, len(required)+len(optional))
	for _, field := range required {
		if !hasAvailableField(settings.AvailableFields, field) {
			return nil, fmt.Errorf("dataset is missing required field %s", field)
		}
		selected = append(selected, field)
	}
	for _, field := range optional {
		if settings.MaxNumberOfFields > 0 && len(selected) >= settings.MaxNumberOfFields {
			break
		}
		if hasAvailableField(settings.AvailableFields, field) {
			selected = append(selected, field)
		}
	}
	return selected, nil
}

func hasAvailableField(fields []string, wanted string) bool {
	for _, field := range fields {
		if strings.EqualFold(field, wanted) {
			return true
		}
		if prefix, suffix, ok := strings.Cut(wanted, "."); ok && strings.EqualFold(field, prefix+"_"+suffix) {
			return true
		}
	}
	return false
}

func numericValue(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
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
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		parsed, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed, err == nil
	}
}

func nonnegativeFinite(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func dimensionValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func contains(fields []string, wanted string) bool {
	for _, field := range fields {
		if field == wanted {
			return true
		}
	}
	return false
}

var _ collector.WindowCollector = (*pageloads)(nil)
var _ collector.WindowCollector = (*webVitals)(nil)
