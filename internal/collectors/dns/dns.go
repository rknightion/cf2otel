package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

const (
	rawDataset    = "dnsAnalyticsAdaptive"
	groupsDataset = "dnsAnalyticsAdaptiveGroups"
	queryLimit    = 10000
)

var rawOptionalFields = []string{
	"responseStale", "sourceIP", "destinationIP",
	"upstreamIP", "ipVersion", "sampleInterval", "querySize", "responseSize",
}

var metricDimensions = []struct {
	field string
	attr  string
}{
	{field: "queryType", attr: semconv.AttrDNSQueryType},
	{field: "responseCode", attr: semconv.AttrDNSResponseCode},
	{field: "responseCached", attr: semconv.AttrDNSResponseCached},
	{field: "responseStale", attr: semconv.AttrDNSResponseStale},
	{field: "protocol", attr: semconv.AttrDNSProtocol},
	{field: "coloName", attr: semconv.AttrDNSColo},
}

var rawLogAttributes = []struct {
	field string
	attr  string
}{
	{field: "queryName", attr: semconv.AttrDNSQueryName},
	{field: "queryType", attr: semconv.AttrDNSQueryType},
	{field: "responseCode", attr: semconv.AttrDNSResponseCode},
	{field: "responseCached", attr: semconv.AttrDNSResponseCached},
	{field: "responseStale", attr: semconv.AttrDNSResponseStale},
	{field: "protocol", attr: semconv.AttrDNSProtocol},
	{field: "coloName", attr: semconv.AttrDNSColo},
	{field: "sourceIP", attr: semconv.AttrDNSSourceIP},
	{field: "destinationIP", attr: semconv.AttrDNSDestinationIP},
	{field: "upstreamIP", attr: semconv.AttrDNSUpstreamIP},
	{field: "ipVersion", attr: semconv.AttrDNSIPVersion},
	{field: "sampleInterval", attr: semconv.AttrDNSSampleInterval},
	{field: "querySize", attr: semconv.AttrDNSQuerySize},
	{field: "responseSize", attr: semconv.AttrDNSResponseSize},
}

type datasetSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type base struct {
	cfg *config.Config
	api cfapi.Client
}

type events struct{ base }
type metrics struct{ base }

func NewEvents(cfg *config.Config, api cfapi.Client) *events {
	return &events{base{cfg: cfg, api: api}}
}

func NewMetrics(cfg *config.Config, api cfapi.Client) *metrics {
	return &metrics{base{cfg: cfg, api: api}}
}

func (*events) Name() string                    { return "dns.events" }
func (*metrics) Name() string                   { return "dns.metrics" }
func (*events) DefaultInterval() time.Duration  { return 5 * time.Minute }
func (*metrics) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*events) Lag() time.Duration              { return 2 * time.Minute }
func (*metrics) Lag() time.Duration             { return 2 * time.Minute }

type dnsEvent struct {
	at    time.Time
	body  string
	attrs []telemetry.Attr
}

func (c *events) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid DNS event window")
	}
	zones, err := c.zones(ctx)
	if err != nil {
		return from, err
	}

	var records []dnsEvent
	var retentionGaps []error
	enabledZones := 0
	for _, zone := range zones {
		settings, err := c.settings(ctx, zone, rawDataset)
		if err != nil {
			return from, fmt.Errorf("DNS event dataset settings: %w", err)
		}
		if !settings.Enabled && len(c.cfg.Cloudflare.Zones) == 0 {
			continue
		}
		wanted, err := fieldsForZone(settings, []string{"datetime", "queryName", "queryType", "responseCode", "responseCached", "protocol", "coloName"}, rawOptionalFields)
		if err != nil {
			return from, fmt.Errorf("zone DNS events: %w", err)
		}
		enabledZones++

		var rows []map[string]any
		req := cfapi.GraphQLRequest{
			Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: rawDataset,
			WantedFields: wanted, From: from, To: to, Limit: queryLimit,
		}
		if err := c.api.Query(ctx, req, &rows); err != nil {
			var gap *cfapi.RetentionGapError
			if errors.As(err, &gap) {
				retentionGaps = append(retentionGaps, fmt.Errorf("zone DNS events: %w", err))
				continue
			}
			return from, fmt.Errorf("zone DNS events: %w", err)
		}
		if len(rows) >= effectiveLimit(settings) {
			return from, errors.New("zone DNS events reached the query limit; narrow the window")
		}
		for _, row := range rows {
			at, ok, err := eventTime(row, from, to)
			if err != nil {
				return from, err
			}
			if !ok {
				continue
			}
			if err := requireRowFields(row, "queryName", "queryType", "responseCode", "responseCached", "protocol", "coloName"); err != nil {
				return from, fmt.Errorf("zone DNS event: %w", err)
			}
			body, err := json.Marshal(row)
			if err != nil {
				return from, fmt.Errorf("encode DNS event: %w", err)
			}
			attrs := []telemetry.Attr{{Key: semconv.AttrDNSZone, Value: zone.Name}}
			for _, field := range rawLogAttributes {
				if value, ok := row[field.field]; ok && value != nil {
					attrs = append(attrs, telemetry.Attr{Key: field.attr, Value: fmt.Sprint(value)})
				}
			}
			records = append(records, dnsEvent{at: at, body: string(body), attrs: attrs})
		}
	}
	if enabledZones == 0 {
		return from, errors.New("DNS event dataset is disabled in every discovered zone")
	}
	if len(retentionGaps) > 0 {
		return from, errors.Join(retentionGaps...)
	}
	for _, record := range records {
		if err := out.LogEvent(ctx, semconv.EventDNSQuery, record.body, record.at, otellog.SeverityInfo, record.attrs...); err != nil {
			return from, err
		}
	}
	return to, nil
}

type dnsMetric struct {
	value float64
	attrs []telemetry.Attr
}

func (c *metrics) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid DNS metrics window")
	}
	zones, err := c.zones(ctx)
	if err != nil {
		return from, err
	}

	var samples []dnsMetric
	var retentionGaps []error
	enabledZones := 0
	for _, zone := range zones {
		settings, err := c.settings(ctx, zone, groupsDataset)
		if err != nil {
			return from, fmt.Errorf("DNS Groups dataset settings: %w", err)
		}
		if !settings.Enabled && len(c.cfg.Cloudflare.Zones) == 0 {
			continue
		}
		wanted, err := fieldsForZone(settings, []string{"count"}, groupOptionalFields())
		if err != nil {
			return from, fmt.Errorf("zone DNS Groups: %w", err)
		}
		enabledZones++

		var rows []map[string]any
		req := cfapi.GraphQLRequest{
			Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: groupsDataset,
			WantedFields: wanted, From: from, To: to, Limit: queryLimit,
		}
		if err := c.api.Query(ctx, req, &rows); err != nil {
			var gap *cfapi.RetentionGapError
			if errors.As(err, &gap) {
				retentionGaps = append(retentionGaps, fmt.Errorf("zone DNS Groups: %w", err))
				continue
			}
			return from, fmt.Errorf("zone DNS Groups: %w", err)
		}
		if len(rows) >= effectiveLimit(settings) {
			return from, errors.New("zone DNS Groups reached the query limit; narrow the window")
		}
		for _, row := range rows {
			count, ok := numericValue(row["count"])
			if !ok || count < 0 {
				return from, errors.New("DNS Groups row is missing a valid nonnegative count")
			}
			if count == 0 {
				continue
			}
			attrs := []telemetry.Attr{{Key: semconv.AttrDNSZone, Value: zone.Name}}
			dimensions, _ := row["dimensions"].(map[string]any)
			for _, dimension := range metricDimensions {
				field := "dimensions." + dimension.field
				if contains(wanted, field) {
					if value := dimensions[dimension.field]; value != nil {
						attrs = append(attrs, telemetry.Attr{Key: dimension.attr, Value: fmt.Sprint(value)})
					}
				}
			}
			samples = append(samples, dnsMetric{value: count, attrs: attrs})
		}
	}
	if enabledZones == 0 {
		return from, errors.New("DNS Groups dataset is disabled in every discovered zone")
	}
	if len(retentionGaps) > 0 {
		return from, errors.Join(retentionGaps...)
	}
	for _, sample := range samples {
		if err := out.Counter(ctx, semconv.MetricDNSQueries, sample.value, sample.attrs...); err != nil {
			return from, err
		}
	}
	return to, nil
}

func groupOptionalFields() []string {
	fields := make([]string, 0, len(metricDimensions))
	for _, dimension := range metricDimensions {
		fields = append(fields, "dimensions."+dimension.field)
	}
	return fields
}

func fieldsForZone(settings cfapi.DatasetSettings, required, optional []string) ([]string, error) {
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

func effectiveLimit(settings cfapi.DatasetSettings) int {
	if settings.MaxPageSize > 0 && settings.MaxPageSize < queryLimit {
		return settings.MaxPageSize
	}
	return queryLimit
}

func (b base) zones(ctx context.Context) ([]cfapi.Zone, error) {
	all, err := b.api.Zones(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DNS zones: %w", err)
	}
	if len(all) == 0 {
		return nil, errors.New("DNS zone discovery returned no zones")
	}
	wanted := b.cfg.Cloudflare.Zones
	if len(wanted) == 0 {
		return all, nil
	}
	selected := make([]cfapi.Zone, 0, len(wanted))
	matched := make([]bool, len(wanted))
	for _, zone := range all {
		included := false
		for i, nameOrID := range wanted {
			if nameOrID == zone.ID || strings.EqualFold(nameOrID, zone.Name) {
				matched[i] = true
				included = true
			}
		}
		if included {
			selected = append(selected, zone)
		}
	}
	for _, found := range matched {
		if !found {
			return nil, errors.New("configured DNS zone absent from discovery")
		}
	}
	return selected, nil
}

func (b base) settings(ctx context.Context, zone cfapi.Zone, dataset string) (cfapi.DatasetSettings, error) {
	reader, ok := b.api.(datasetSettingsReader)
	if !ok {
		return cfapi.DatasetSettings{}, errors.New("Cloudflare client does not expose dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.ZoneScope, zone.ID, dataset)
	if err != nil {
		return cfapi.DatasetSettings{}, fmt.Errorf("read %s settings: %w", dataset, err)
	}
	return settings, nil
}

func eventTime(row map[string]any, from, to time.Time) (time.Time, bool, error) {
	value, ok := row["datetime"].(string)
	if !ok || value == "" {
		return time.Time{}, false, errors.New("DNS event is missing a valid datetime")
	}
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false, errors.New("DNS event is missing a valid datetime")
	}
	return at, !at.Before(from) && at.Before(to), nil
}

func requireRowFields(row map[string]any, fields ...string) error {
	for _, field := range fields {
		if row[field] == nil {
			return fmt.Errorf("DNS event is missing required field %s", field)
		}
	}
	return nil
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

func contains(fields []string, wanted string) bool {
	for _, field := range fields {
		if field == wanted {
			return true
		}
	}
	return false
}

var _ collector.WindowCollector = (*events)(nil)
var _ collector.WindowCollector = (*metrics)(nil)
