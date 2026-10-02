package httpreq

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

var (
	routeNumber = regexp.MustCompile(`^[0-9]+$`)
	routeUUID   = regexp.MustCompile(`^[A-Fa-f0-9]{8}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{12}$`)
	routeHex    = regexp.MustCompile(`^[A-Fa-f0-9]{8,}$`)
)

// This value is only a lookup key. Neither raw nor normalized paths are labels.
func normalizedErrorPath(raw string) (string, bool) {
	if len(raw) > 4096 {
		return "", false
	}
	raw = strings.SplitN(strings.SplitN(raw, "?", 2)[0], "#", 2)[0]
	path, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(path) || len(path) > 4096 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", false
	}
	for _, r := range path {
		if r <= ' ' || r == 127 || r == '\\' {
			return "", false
		}
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "." || part == ".." {
			return "", false
		}
		switch {
		case routeNumber.MatchString(part):
			parts[i] = ":number"
		case routeUUID.MatchString(part):
			parts[i] = ":uuid"
		case routeHex.MatchString(part):
			parts[i] = ":hex"
		}
	}
	return strings.Join(parts, "/"), true
}

type highSpec struct {
	toggle, alias, metric string
	fields, attrs         []string
}

var highSpecs = []highSpec{
	{"colo", "high_colo", semconv.MetricHTTPRequestsByColo, []string{"coloCode"}, []string{semconv.AttrHTTPColo}},
	{"asn", "high_asn", semconv.MetricHTTPRequestsByASN, []string{"clientAsn", "clientASNDescription"}, []string{semconv.AttrHTTPClientASN, semconv.AttrHTTPClientASNDescription}},
	{"error_path", "high_error_route", semconv.MetricHTTPErrorsByRoute, []string{"clientRequestPath", "edgeResponseStatus"}, []string{semconv.AttrHTTPRouteName, semconv.AttrHTTPStatusClass}},
}

type highKey struct {
	first, second string
	remainder     bool
}

func (c metrics) collectHighCardinality(ctx context.Context, zone cfapi.Zone, from, to time.Time) ([]breakdownPoint, error) {
	enabled := map[string]bool{}
	for _, name := range c.cfg.HTTP.Breakdowns {
		enabled[name] = true
	}
	if !enabled["colo"] && !enabled["asn"] && !enabled["error_path"] {
		return nil, nil
	}
	if c.cfg.HTTP.HighCardinalityLimit < 1 || c.cfg.HTTP.HighCardinalityLimit > 5000 {
		return nil, errors.New("HTTP high-cardinality limit is invalid")
	}
	batch, ok := c.api.(cfapi.GraphQLBatchQuerier)
	if !ok {
		return nil, errors.New("HTTP high-cardinality breakdowns require batch support")
	}
	provider, ok := c.api.(httpGroupSettingsProvider)
	if !ok {
		return nil, errors.New("HTTP groups settings discovery is unavailable")
	}
	settings, err := provider.DatasetSettings(ctx, cfapi.ZoneScope, zone.ID, "httpRequestsAdaptiveGroups")
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, errors.New("HTTP groups dataset is disabled")
	}
	available := map[string]bool{}
	for _, field := range settings.AvailableFields {
		available[httpGroupFieldName(field)] = true
	}
	hosts := map[string]bool{}
	for _, host := range c.cfg.HTTP.HighCardinalityHosts {
		hosts[host] = true
	}
	pageLimit := 10000
	if settings.MaxPageSize > 0 && settings.MaxPageSize < pageLimit {
		pageLimit = settings.MaxPageSize
	}
	var selections []cfapi.GraphQLBatchSelection
	var specs []highSpec
	for _, spec := range highSpecs {
		if !enabled[spec.toggle] {
			continue
		}
		fields := []string{"count"}
		for _, field := range spec.fields {
			fields = append(fields, "dimensions."+field)
		}
		if len(hosts) > 0 {
			fields = append(fields, "dimensions.clientRequestHTTPHost")
		}
		eligible := settings.MaxNumberOfFields <= 0 || len(fields) <= settings.MaxNumberOfFields
		for _, field := range fields {
			eligible = eligible && available[httpGroupFieldName(field)]
		}
		if !eligible {
			continue
		}
		// Keep each selection's source policy independent. Only error-route
		// groups need the live-verified status range, so healthy path groups
		// cannot saturate that query before local defensive validation.
		filter := map[string]any{}
		for key, value := range c.requestSourceFilter() {
			filter[key] = value
		}
		if spec.toggle == "error_path" {
			filter["edgeResponseStatus_geq"] = 400
			filter["edgeResponseStatus_leq"] = 599
		}
		specs = append(specs, spec)
		selections = append(selections, cfapi.GraphQLBatchSelection{Alias: spec.alias, Request: cfapi.GraphQLRequest{Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: "httpRequestsAdaptiveGroups", WantedFields: fields, From: from, To: to, Limit: pageLimit, Filter: filter}})
	}
	if len(selections) == 0 {
		return nil, nil
	}
	// QueryBatch deliberately does not split duration windows. Query disjoint
	// periods here and sum their groups only after every period has validated.
	result := map[string]json.RawMessage{}
	periodRows := map[string][]json.RawMessage{}
	for start := from; start.Before(to); {
		end := to
		if settings.MaxDuration > 0 && to.Sub(start).Seconds() > float64(settings.MaxDuration) {
			end = start.Add(time.Duration(settings.MaxDuration) * time.Second)
		}
		for i := range selections {
			selections[i].Request.From = start
			selections[i].Request.To = end
		}
		part, err := batch.QueryBatch(ctx, selections)
		if err != nil {
			return nil, err
		}
		for _, spec := range specs {
			raw, ok := part[spec.alias]
			if !ok || len(raw) == 0 || raw[0] != '[' {
				return nil, errors.New("HTTP high-cardinality query missing row array")
			}
			var rows []json.RawMessage
			if err := json.Unmarshal(raw, &rows); err != nil {
				return nil, errors.New("HTTP high-cardinality query has invalid rows")
			}
			if len(rows) >= pageLimit {
				return nil, errors.New("HTTP high-cardinality query reached the query limit")
			}
			periodRows[spec.alias] = append(periodRows[spec.alias], rows...)
			if len(periodRows[spec.alias]) >= 10000 {
				return nil, errors.New("HTTP high-cardinality window reached the row limit")
			}
		}
		start = end
	}
	for _, spec := range specs {
		rows := periodRows[spec.alias]
		if rows == nil {
			rows = []json.RawMessage{}
		}
		result[spec.alias], err = json.Marshal(rows)
		if err != nil {
			return nil, errors.New("HTTP high-cardinality rows could not be assembled")
		}
	}
	routes := map[string]string{}
	for name, template := range c.cfg.HTTP.ErrorPathRoutes {
		routes[template] = name
	}
	var points []breakdownPoint
	for _, spec := range specs {
		raw, ok := result[spec.alias]
		if !ok || len(raw) == 0 || raw[0] != '[' {
			return nil, errors.New("HTTP high-cardinality query missing row array")
		}
		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, errors.New("HTTP high-cardinality query has invalid rows")
		}
		if len(rows) >= 10000 {
			return nil, errors.New("HTTP high-cardinality query reached the query limit")
		}
		groups := map[highKey]float64{}
		total := float64(0)
		for _, row := range rows {
			count, valid := metricNumber(row["count"])
			if !valid || count < 0 {
				return nil, errors.New("HTTP high-cardinality query has invalid count")
			}
			total += count
			if math.IsInf(total, 0) || math.IsNaN(total) {
				return nil, errors.New("HTTP high-cardinality source count overflow")
			}
			dims, ok := row["dimensions"].(map[string]any)
			if !ok {
				return nil, errors.New("HTTP high-cardinality query missing dimensions")
			}
			hostAllowed := true
			if len(hosts) > 0 {
				host, ok := dims["clientRequestHTTPHost"].(string)
				if !ok {
					return nil, errors.New("HTTP high-cardinality query requires string host")
				}
				hostAllowed = hosts[strings.ToLower(host)]
			}
			key := highKey{}
			first, ok := dims[spec.fields[0]].(string)
			if !ok {
				return nil, errors.New("HTTP high-cardinality query requires string dimension")
			}
			key.first = first
			if spec.toggle == "asn" {
				key.second, ok = dims[spec.fields[1]].(string)
				if !ok {
					return nil, errors.New("HTTP high-cardinality query requires string ASN description")
				}
			}
			if spec.toggle == "error_path" {
				status, valid := metricNumber(dims["edgeResponseStatus"])
				if !valid || status < 0 || status > 65535 || math.Trunc(status) != status {
					return nil, errors.New("HTTP high-cardinality query has invalid status")
				}
				if status < 400 || status >= 600 {
					continue
				}
				key.second = "4xx"
				if status >= 500 {
					key.second = "5xx"
				}
				template, valid := normalizedErrorPath(first)
				key.first = "other"
				key.remainder = true
				if name, matched := routes[template]; valid && matched {
					key.first = name
					key.remainder = false
				}
			}
			if !hostAllowed {
				continue
			}
			value := groups[key] + count
			if math.IsInf(value, 0) || math.IsNaN(value) {
				return nil, errors.New("HTTP high-cardinality count overflow")
			}
			if count > 0 {
				groups[key] = value
			}
		}
		keys := make([]highKey, 0, len(groups))
		for key := range groups {
			if !key.remainder {
				keys = append(keys, key)
			}
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].first != keys[j].first {
				return keys[i].first < keys[j].first
			}
			return keys[i].second < keys[j].second
		})
		remainder := float64(0)
		for key, count := range groups {
			if key.remainder {
				remainder += count
			}
		}
		for i, key := range keys {
			if i >= c.cfg.HTTP.HighCardinalityLimit {
				remainder += groups[key]
				continue
			}
			attrs := []telemetry.Attr{{Key: semconv.AttrHTTPZone, Value: zone.Name}, {Key: spec.attrs[0], Value: key.first}, {Key: semconv.AttrHTTPBreakdownRemainder, Value: "false"}}
			if len(spec.attrs) > 1 {
				attrs = append(attrs, telemetry.Attr{Key: spec.attrs[1], Value: key.second})
			}
			points = append(points, breakdownPoint{spec.metric, groups[key], attrs})
		}
		if math.IsInf(remainder, 0) || math.IsNaN(remainder) {
			return nil, errors.New("HTTP high-cardinality remainder overflow")
		}
		if remainder > 0 {
			attrs := []telemetry.Attr{{Key: semconv.AttrHTTPZone, Value: zone.Name}, {Key: spec.attrs[0], Value: "other"}, {Key: semconv.AttrHTTPBreakdownRemainder, Value: "true"}}
			if len(spec.attrs) > 1 {
				attrs = append(attrs, telemetry.Attr{Key: spec.attrs[1], Value: "other"})
			}
			points = append(points, breakdownPoint{spec.metric, remainder, attrs})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return points, nil
}
