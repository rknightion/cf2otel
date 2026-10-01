package httpreq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type breakdownDimension struct{ toggle, field, attr string }
type breakdownSpec struct {
	alias, metric string
	dimensions    []breakdownDimension
	bytes         bool
}

var httpBreakdownSpecs = []breakdownSpec{
	{"status", semconv.MetricHTTPRequestsByStatus, []breakdownDimension{{"status", "edgeResponseStatus", semconv.AttrHTTPStatusCode}, {"origin_status", "originResponseStatus", semconv.AttrHTTPOriginStatusCode}}, false},
	{"country", semconv.MetricHTTPRequestsByCountry, []breakdownDimension{{"country", "clientCountryName", semconv.AttrHTTPClientCountry}}, true},
	{"protocol", semconv.MetricHTTPRequestsByProtocol, []breakdownDimension{{"protocol", "clientRequestHTTPProtocol", semconv.AttrHTTPProtocol}, {"tls_protocol", "clientSSLProtocol", semconv.AttrHTTPTLSProtocol}}, false},
	{"method", semconv.MetricHTTPRequestsByMethod, []breakdownDimension{{"method", "clientRequestHTTPMethodName", semconv.AttrHTTPMethod}}, false},
	{"content_type", semconv.MetricHTTPRequestsByContentType, []breakdownDimension{{"content_type", "edgeResponseContentTypeName", semconv.AttrHTTPContentType}}, false},
}

type breakdownPoint struct {
	name  string
	value float64
	attrs []telemetry.Attr
}

func (c metrics) collectBreakdowns(ctx context.Context, zone cfapi.Zone, from, to time.Time) ([]breakdownPoint, error) {
	if len(c.cfg.HTTP.Breakdowns) == 0 {
		return nil, nil
	}
	batch, ok := c.api.(cfapi.GraphQLBatchQuerier)
	if !ok {
		return nil, errors.New("HTTP breakdowns require GraphQL batch support")
	}
	provider, ok := c.api.(httpGroupSettingsProvider)
	if !ok {
		return nil, errors.New("HTTP groups settings discovery is unavailable")
	}
	settings, err := provider.DatasetSettings(ctx, cfapi.ZoneScope, zone.ID, "httpRequestsAdaptiveGroups")
	if err != nil {
		return nil, fmt.Errorf("HTTP breakdown settings: %w", err)
	}
	if !settings.Enabled {
		return nil, errors.New("HTTP groups dataset is disabled")
	}
	available := map[string]bool{}
	for _, field := range settings.AvailableFields {
		available[httpGroupFieldName(field)] = true
	}
	enabled := map[string]bool{}
	for _, name := range c.cfg.HTTP.Breakdowns {
		enabled[name] = true
	}
	var selections []cfapi.GraphQLBatchSelection
	var specs []breakdownSpec
	for _, spec := range httpBreakdownSpecs {
		fields := []string{"count"}
		active := spec
		active.dimensions = nil
		active.bytes = false
		if !available["count"] {
			return nil, errors.New("HTTP breakdown count field unavailable")
		}
		for _, dim := range spec.dimensions {
			if !enabled[dim.toggle] || !available[httpGroupFieldName("dimensions."+dim.field)] {
				continue
			}
			if settings.MaxNumberOfFields > 0 && len(fields) >= settings.MaxNumberOfFields {
				continue
			}
			fields = append(fields, "dimensions."+dim.field)
			active.dimensions = append(active.dimensions, dim)
		}
		if len(active.dimensions) == 0 {
			continue
		}
		if spec.bytes && available[httpGroupFieldName("sum.edgeResponseBytes")] && (settings.MaxNumberOfFields <= 0 || len(fields) < settings.MaxNumberOfFields) {
			fields = append(fields, "sum.edgeResponseBytes")
			active.bytes = true
		}
		specs = append(specs, active)
		selections = append(selections, cfapi.GraphQLBatchSelection{Alias: spec.alias, Request: cfapi.GraphQLRequest{Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: "httpRequestsAdaptiveGroups", WantedFields: fields, From: from, To: to, Limit: 10000, Filter: c.requestSourceFilter()}})
	}
	if len(selections) == 0 {
		return nil, nil
	}
	result, err := batch.QueryBatch(ctx, selections)
	if err != nil {
		return nil, fmt.Errorf("zone HTTP breakdowns: %w", err)
	}
	var points []breakdownPoint
	for _, spec := range specs {
		raw, ok := result[spec.alias]
		if !ok || len(raw) == 0 || raw[0] != '[' {
			return nil, errors.New("HTTP breakdown batch missing row array")
		}
		var rows []map[string]any
		if err = json.Unmarshal(raw, &rows); err != nil {
			return nil, err
		}
		if len(rows) >= 10000 {
			return nil, errors.New("HTTP breakdown query reached the query limit; narrow the window")
		}
		seen := map[string]bool{}
		for _, row := range rows {
			count, valid := metricNumber(row["count"])
			if !valid || count < 0 {
				return nil, errors.New("HTTP breakdown invalid count")
			}
			dims, _ := row["dimensions"].(map[string]any)
			attrs := []telemetry.Attr{{Key: semconv.AttrHTTPZone, Value: zone.Name}}
			values := []string{}
			for _, dim := range spec.dimensions {
				if dims[dim.field] == nil {
					return nil, errors.New("HTTP breakdown missing selected dimension")
				}
				value := str(dims[dim.field])
				if dim.toggle == "country" {
					value = strings.ToUpper(value)
				}
				attrs = append(attrs, telemetry.Attr{Key: dim.attr, Value: value})
				values = append(values, value)
			}
			key, _ := json.Marshal(values)
			if seen[string(key)] {
				return nil, errors.New("HTTP breakdown duplicate dimension group")
			}
			seen[string(key)] = true
			if count == 0 {
				continue
			}
			points = append(points, breakdownPoint{spec.metric, count, attrs})
			if spec.bytes {
				sum, _ := row["sum"].(map[string]any)
				bytes, valid := metricNumber(sum["edgeResponseBytes"])
				if !valid || bytes < 0 {
					return nil, errors.New("HTTP breakdown invalid response bytes")
				}
				points = append(points, breakdownPoint{semconv.MetricHTTPResponseBytesByCountry, bytes, attrs})
			}
			if len(points) > c.cfg.HTTP.MaxMetricSeriesPerWindow {
				return nil, fmt.Errorf("HTTP metric series count exceeds the per-window limit %d", c.cfg.HTTP.MaxMetricSeriesPerWindow)
			}
		}
	}
	return points, nil
}
