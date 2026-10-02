package firewall

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// Exporter-defined numeric intervals, not vendor classifications or sentinels.
func firewallBotScoreBucket(value any) (string, bool) {
	// Do not coerce string values or missing/null values into scores.
	switch value.(type) {
	case float64, float32, int, int64, json.Number:
	default:
		return "", false
	}
	score, ok := numericValue(value)
	if !ok || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 255 || math.Trunc(score) != score {
		return "", false
	}
	lower := int(score) / 10 * 10
	return fmt.Sprintf("%d-%d", lower, min(lower+9, 255)), true
}

// Source names pass through without semantic guesses. Admissions are global to
// this collector and sticky across windows, with one reserved overflow value.
func firewallBotScoreSource(value any, sources map[string]struct{}) (string, bool) {
	name, ok := value.(string)
	if !ok || name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	if name == "other" {
		return name, true
	}
	if _, admitted := sources[name]; admitted {
		return name, true
	}
	if len(sources) >= 32 {
		return "other", true
	}
	sources[name] = struct{}{}
	return name, true
}

type metricDimension struct{ field, key string }

var ruleMetricDimensions = []metricDimension{
	{"ruleId", semconv.AttrFirewallRuleID},
	{"clientRequestHTTPHost", semconv.AttrFirewallHost},
	{"clientCountryName", semconv.AttrFirewallCountry},
}

type ruleCache struct {
	expires      time.Time
	descriptions map[string]string
}

// Cache both successful and failed lookups per zone to avoid retry storms. Ruleset
// metadata is optional: errors never stop collection or checkpoint advancement.
func (c *metrics) ruleDescriptions(ctx context.Context, zone string) map[string]string {
	c.rulesMu.Lock()
	defer c.rulesMu.Unlock()
	now := c.now()
	if entry, ok := c.rules[zone]; ok && now.Before(entry.expires) {
		return entry.descriptions
	}
	descriptions := make(map[string]string)
	base := "/zones/" + url.PathEscape(zone) + "/rulesets"
	var sets []struct {
		ID    string `json:"id"`
		Phase string `json:"phase"`
	}
	if err := c.api.Get(ctx, base, nil, &sets); err == nil {
		sort.Slice(sets, func(i, j int) bool { return sets[i].ID < sets[j].ID })
		for _, set := range sets {
			if set.ID == "" || (set.Phase != "http_request_firewall_custom" && set.Phase != "http_request_firewall_managed") {
				continue
			}
			var detail struct {
				Rules []struct {
					ID          string `json:"id"`
					Description string `json:"description"`
				} `json:"rules"`
			}
			if err := c.api.Get(ctx, base+"/"+url.PathEscape(set.ID), nil, &detail); err != nil {
				continue
			}
			for _, rule := range detail.Rules {
				if rule.ID != "" && rule.Description != "" {
					descriptions[rule.ID] = rule.Description
				}
			}
		}
	}
	c.rules[zone] = ruleCache{expires: now.Add(time.Hour), descriptions: descriptions}
	return descriptions
}

// Aggregate identical source series before bounding. Prefer legacy points, then
// lexicographically ordered enrichment. A single all-other remainder accounts
// for discarded source counts exactly once, including the cap=1 case.
func boundFirewallMetrics(samples []firewallMetric, cap int) []firewallMetric {
	type point struct {
		firewallMetric
		key      string
		enriched bool
	}
	points := make(map[string]*point)
	for _, sample := range samples {
		sort.Slice(sample.attrs, func(i, j int) bool { return sample.attrs[i].Key < sample.attrs[j].Key })
		encoded, _ := json.Marshal(sample.attrs)
		key := string(encoded)
		if p := points[key]; p != nil {
			p.value += sample.value
			continue
		}
		p := &point{firewallMetric: sample, key: key}
		for _, attr := range sample.attrs {
			switch attr.Key {
			case semconv.AttrFirewallRuleID, semconv.AttrFirewallHost, semconv.AttrFirewallCountry, semconv.AttrFirewallRuleDescription, semconv.AttrFirewallBotScoreBucket, semconv.AttrFirewallBotScoreSource:
				p.enriched = true
			}
		}
		points[key] = p
	}
	ordered := make([]*point, 0, len(points))
	for _, p := range points {
		ordered = append(ordered, p)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].enriched != ordered[j].enriched {
			return !ordered[i].enriched
		}
		return ordered[i].key < ordered[j].key
	})
	keep := len(ordered)
	if keep > cap {
		keep = cap - 1
	}
	result := make([]firewallMetric, 0, min(len(ordered), cap))
	for _, p := range ordered[:keep] {
		result = append(result, p.firewallMetric)
	}
	if keep < len(ordered) {
		var discarded float64
		for _, p := range ordered[keep:] {
			discarded += p.value
		}
		attrs := []telemetry.Attr{}
		for _, key := range []string{semconv.AttrFirewallZone, semconv.AttrFirewallAction, semconv.AttrFirewallSource, semconv.AttrFirewallRuleID, semconv.AttrFirewallRuleDescription, semconv.AttrFirewallHost, semconv.AttrFirewallCountry, semconv.AttrFirewallBotScoreBucket, semconv.AttrFirewallBotScoreSource} {
			attrs = append(attrs, telemetry.Attr{Key: key, Value: "other"})
		}
		result = append(result, firewallMetric{value: discarded, attrs: attrs})
	}
	return result
}
