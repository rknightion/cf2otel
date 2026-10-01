package collector

import (
	"context"
	"strings"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

var zoneReasons = [...]string{"include", "exclude", "unentitled", "no_account", "other"}

// ZoneSelection applies exclusions only to a caller's already validated include
// and ownership selection. IDs match exactly; names match case-insensitively.
// It never changes the discovery result or validates a collector's selectors.
type ZoneSelection struct {
	Zones      []cfapi.Zone
	Discovered int
	Filtered   map[string]int
}

func SelectZones(all, eligible []cfapi.Zone, exclude []string, account string) ZoneSelection {
	result := ZoneSelection{Discovered: len(all), Filtered: map[string]int{}}
	included := map[string]bool{}
	for _, zone := range eligible {
		included[zone.ID] = true
	}
	for _, zone := range all {
		if included[zone.ID] {
			continue
		}
		reason := "include"
		if account != "" && zone.Account.ID != account {
			reason = "no_account"
		}
		result.Filtered[reason]++
	}
	for _, zone := range eligible {
		excluded := false
		for _, selector := range exclude {
			if selector == zone.ID || strings.EqualFold(selector, zone.Name) {
				excluded = true
				break
			}
		}
		if excluded {
			result.Filtered["exclude"]++
			continue
		}
		result.Zones = append(result.Zones, zone)
	}
	return result
}

type zonePollKey struct{}

// ZonePoll is per-collection state, never shared between concurrent collectors.
// Processed records a zone once even when queries split or select many datasets.
type ZonePoll struct {
	selection  ZoneSelection
	discovered bool
	processed  map[string]bool
	skipped    map[string]string
}

func StartZonePoll(ctx context.Context) (context.Context, *ZonePoll) {
	poll := &ZonePoll{processed: map[string]bool{}, skipped: map[string]string{}}
	return context.WithValue(ctx, zonePollKey{}, poll), poll
}

// SelectPollZones is the adapter from pure selection to per-run observation.
// Call it after all legacy include/ownership validation has succeeded.
func SelectPollZones(ctx context.Context, all, eligible []cfapi.Zone, exclude []string, account string) []cfapi.Zone {
	selection := SelectZones(all, eligible, exclude, account)
	if poll, ok := ctx.Value(zonePollKey{}).(*ZonePoll); ok {
		poll.selection = selection
		poll.discovered = true
	}
	return selection.Zones
}
func (p *ZonePoll) Process(id string) { p.processed[id] = true }
func (p *ZonePoll) Skip(id, reason string) {
	for _, bounded := range zoneReasons {
		if reason == bounded {
			p.skipped[id] = reason
			return
		}
	}
	p.skipped[id] = "other"
}

// Finish emits complete polls only, preserving collectors' no-partial-output
// behavior on errors. It resets every bounded reason series, including zeroes.
// Emitter failures remain collection failures, so windows are not committed.
func (p *ZonePoll) Finish(ctx context.Context, out telemetry.Emitter, name string, err *error) {
	if *err != nil || !p.discovered {
		return
	}
	attrs := []telemetry.Attr{{Key: semconv.AttrCollector, Value: name}}
	skipped := map[string]int{}
	seen := map[string]bool{}
	for _, zone := range p.selection.Zones {
		if seen[zone.ID] || p.processed[zone.ID] {
			continue
		}
		seen[zone.ID] = true
		reason := p.skipped[zone.ID]
		if reason == "" {
			reason = "other"
		}
		skipped[reason]++
	}
	for _, point := range []struct {
		name  string
		value int
	}{{semconv.MetricZonesDiscovered, p.selection.Discovered}, {semconv.MetricZonesProcessed, len(p.processed)}} {
		if emitErr := out.Gauge(ctx, point.name, float64(point.value), attrs...); emitErr != nil {
			*err = emitErr
			return
		}
	}
	for _, reason := range zoneReasons {
		labels := append([]telemetry.Attr{}, attrs...)
		labels = append(labels, telemetry.Attr{Key: semconv.AttrZoneReason, Value: reason})
		for _, point := range []struct {
			name  string
			value int
		}{{semconv.MetricZonesFiltered, p.selection.Filtered[reason]}, {semconv.MetricZonesSkipped, skipped[reason]}} {
			if emitErr := out.Gauge(ctx, point.name, float64(point.value), labels...); emitErr != nil {
				*err = emitErr
				return
			}
		}
	}
}
