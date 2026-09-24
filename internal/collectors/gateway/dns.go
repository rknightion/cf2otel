package gateway

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
	gatewayDNSDataset        = "cf1GatewayDnsRawGroups"
	gatewayDNSQueryLimit     = 10000
	gatewayDNSOther          = "other"
	gatewayDNSUnknownCountry = "ZZ"
)

var gatewayDNSFields = []string{
	"sum.queries",
	"dimensions.queryType",
	"dimensions.resolverDecision",
	"dimensions.country",
}

type gatewayDatasetSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type dnsMetrics struct {
	cfg *config.Config
	api cfapi.Client
}

func NewDNSMetrics(cfg *config.Config, api cfapi.Client) *dnsMetrics {
	return &dnsMetrics{cfg: cfg, api: api}
}

func (*dnsMetrics) Name() string                   { return "gateway.dns" }
func (*dnsMetrics) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*dnsMetrics) Lag() time.Duration             { return 2 * time.Minute }

type gatewayDNSMetric struct {
	value float64
	attrs []telemetry.Attr
}

func (c *dnsMetrics) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid Gateway DNS metrics window")
	}
	if c.cfg == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("Gateway DNS requires a configured Cloudflare account")
	}
	reader, ok := c.api.(gatewayDatasetSettingsReader)
	if !ok {
		return from, errors.New("Cloudflare client does not expose Gateway DNS dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, gatewayDNSDataset)
	if err != nil {
		return from, fmt.Errorf("read Gateway DNS dataset settings: %w", err)
	}
	if !settings.Enabled {
		return from, errors.New("Gateway DNS Groups dataset is disabled")
	}
	for _, field := range gatewayDNSFields {
		if !gatewayHasAvailableField(settings.AvailableFields, field) {
			return from, fmt.Errorf("Gateway DNS Groups dataset is missing required field %s", field)
		}
	}
	if settings.MaxNumberOfFields <= 0 || settings.MaxNumberOfFields < len(gatewayDNSFields) {
		return from, errors.New("Gateway DNS Groups dataset has an invalid field limit")
	}
	if settings.MaxPageSize <= 0 {
		return from, errors.New("Gateway DNS Groups dataset is missing its page-size limit")
	}
	if settings.MaxDuration <= 0 || settings.NotOlderThan <= 0 {
		return from, errors.New("Gateway DNS Groups dataset is missing its duration or retention limit")
	}

	var rows []map[string]any
	request := cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      gatewayDNSDataset,
		WantedFields: append([]string(nil), gatewayDNSFields...),
		From:         from,
		To:           to,
		Limit:        gatewayDNSQueryLimit,
	}
	if err := c.api.Query(ctx, request, &rows); err != nil {
		return from, fmt.Errorf("query Gateway DNS Groups dataset: %w", err)
	}
	if len(rows) >= gatewayDNSQueryLimit {
		return from, errors.New("Gateway DNS Groups result reached the collector limit")
	}

	samples := make([]gatewayDNSMetric, 0, len(rows))
	for _, row := range rows {
		sum, ok := row["sum"].(map[string]any)
		if !ok {
			return from, errors.New("Gateway DNS Groups row is missing sum fields")
		}
		count, ok := gatewayDNSCount(sum["queries"])
		if !ok || count <= 0 {
			return from, errors.New("Gateway DNS Groups row has an invalid query sum")
		}
		dimensions, ok := row["dimensions"].(map[string]any)
		if !ok {
			return from, errors.New("Gateway DNS Groups row is missing required dimensions")
		}
		queryType, ok := gatewayDNSDimension(dimensions["queryType"])
		if !ok {
			return from, errors.New("Gateway DNS Groups row is missing queryType")
		}
		decision, ok := gatewayDNSDimension(dimensions["resolverDecision"])
		if !ok {
			return from, errors.New("Gateway DNS Groups row is missing resolverDecision")
		}
		country, ok := gatewayDNSDimension(dimensions["country"])
		if !ok {
			return from, errors.New("Gateway DNS Groups row is missing country")
		}
		samples = append(samples, gatewayDNSMetric{
			value: count,
			attrs: []telemetry.Attr{
				{Key: semconv.AttrGatewayDNSQueryType, Value: boundedGatewayDNSQueryType(queryType)},
				{Key: semconv.AttrGatewayDNSDecision, Value: boundedGatewayDNSDecision(decision)},
				{Key: semconv.AttrGatewayDNSCountry, Value: boundedGatewayDNSCountry(country)},
			},
		})
	}

	for _, sample := range samples {
		if err := out.Counter(ctx, semconv.MetricGatewayDNSQueries, sample.value, sample.attrs...); err != nil {
			return from, err
		}
	}
	return to, nil
}

func gatewayHasAvailableField(available []string, wanted string) bool {
	for _, field := range available {
		if strings.EqualFold(field, wanted) {
			return true
		}
		if prefix, suffix, ok := strings.Cut(wanted, "."); ok && strings.EqualFold(field, prefix+"_"+suffix) {
			return true
		}
	}
	return false
}

func gatewayDNSCount(value any) (float64, bool) {
	var count float64
	switch number := value.(type) {
	case float64:
		count = number
	case float32:
		count = float64(number)
	case int:
		count = float64(number)
	case int32:
		count = float64(number)
	case int64:
		count = float64(number)
	case json.Number:
		parsed, err := number.Float64()
		if err != nil {
			return 0, false
		}
		count = parsed
	case string:
		parsed, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return 0, false
		}
		count = parsed
	default:
		return 0, false
	}
	return count, !math.IsNaN(count) && !math.IsInf(count, 0) && math.Trunc(count) == count
}

func gatewayDNSDimension(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	var text string
	switch dimension := value.(type) {
	case string:
		text = dimension
	case json.Number:
		text = dimension.String()
	case int:
		text = strconv.Itoa(dimension)
	case int32:
		text = strconv.FormatInt(int64(dimension), 10)
	case int64:
		text = strconv.FormatInt(dimension, 10)
	case uint:
		text = strconv.FormatUint(uint64(dimension), 10)
	case uint32:
		text = strconv.FormatUint(uint64(dimension), 10)
	case uint64:
		text = strconv.FormatUint(dimension, 10)
	case float64:
		if math.IsNaN(dimension) || math.IsInf(dimension, 0) || math.Trunc(dimension) != dimension {
			return "", false
		}
		text = strconv.FormatFloat(dimension, 'f', 0, 64)
	default:
		return "", false
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

func boundedGatewayDNSQueryType(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "A", "1":
		return "A"
	case "NS", "2":
		return "NS"
	case "CNAME", "5":
		return "CNAME"
	case "SOA", "6":
		return "SOA"
	case "PTR", "12":
		return "PTR"
	case "MX", "15":
		return "MX"
	case "TXT", "16":
		return "TXT"
	case "AAAA", "28":
		return "AAAA"
	case "SRV", "33":
		return "SRV"
	case "DS", "43":
		return "DS"
	case "RRSIG", "46":
		return "RRSIG"
	case "DNSKEY", "48":
		return "DNSKEY"
	case "CAA", "257":
		return "CAA"
	case "SVCB", "64":
		return "SVCB"
	case "HTTPS", "65":
		return "HTTPS"
	case "ANY", "255":
		return "ANY"
	default:
		return gatewayDNSOther
	}
}

func boundedGatewayDNSDecision(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow", "allowed", "resolve", "resolved", "permit", "permitted":
		return "allow"
	case "block", "blocked", "deny", "denied":
		return "block"
	case "override", "overridden":
		return "override"
	case "bypass", "bypassed":
		return "bypass"
	case "sinkhole", "sinkholed":
		return "sinkhole"
	case "safe_search", "safesearch":
		return "safe_search"
	case "isolate", "isolated":
		return "isolate"
	case "unknown", "none":
		return "unknown"
	default:
		return gatewayDNSOther
	}
}

func boundedGatewayDNSCountry(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) != 2 || value[0] < 'A' || value[0] > 'Z' || value[1] < 'A' || value[1] > 'Z' {
		return gatewayDNSUnknownCountry
	}
	return value
}

var _ collector.WindowCollector = (*dnsMetrics)(nil)
