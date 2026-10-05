package statuspage

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type componentCollector struct {
	source   *source
	interval time.Duration
}

func (*componentCollector) Name() string                   { return semconv.CollectorNameStatuspageComponents }
func (*componentCollector) DefaultInterval() time.Duration { return 5 * time.Minute }

func statusValue(status string) float64 {
	switch status {
	case "operational":
		return 0
	case "under_maintenance":
		return 1
	case "degraded_performance":
		return 2
	case "partial_outage":
		return 3
	case "major_outage":
		return 4
	default:
		return 5
	}
}

type labelKey struct{ name, kind string }

func (c *componentCollector) Collect(ctx context.Context, e telemetry.Emitter) error {
	batch, batchOK := e.(telemetry.SnapshotBatchEmitter)
	single, singleOK := e.(telemetry.SnapshotEmitter)
	if !batchOK && !singleOK {
		return errors.New("statuspage components require snapshot publication")
	}
	if c.interval <= 0 || c.interval > time.Duration(1<<63-1)/3 {
		return errors.New("invalid statuspage component snapshot interval")
	}
	b, err := c.source.fetch(ctx, "/api/v2/summary.json")
	if err != nil {
		return err
	}
	rows, err := decodeComponents(b)
	if err != nil {
		return err
	}
	values := make(map[labelKey]float64, len(rows))
	for _, r := range rows {
		kind := "component"
		if *r.Group {
			kind = "group"
		}
		key := labelKey{*r.Name, kind}
		v := statusValue(*r.Status)
		if previous, ok := values[key]; !ok || v > previous {
			values[key] = v
		}
	}
	keys := make([]labelKey, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].name == keys[j].name {
			return keys[i].kind < keys[j].kind
		}
		return keys[i].name < keys[j].name
	})
	points := make([]telemetry.GaugePoint, 0, min(len(keys), c.source.config.ComponentCap)+1)
	overflow := float64(0)
	for i, k := range keys {
		if i >= c.source.config.ComponentCap {
			overflow = max(overflow, values[k])
			continue
		}
		points = append(points, telemetry.GaugePoint{Value: values[k], Attrs: []telemetry.Attr{{Key: semconv.AttrStatusComponentName, Value: k.name}, {Key: semconv.AttrStatusComponentType, Value: k.kind}}})
	}
	if len(keys) > c.source.config.ComponentCap {
		points = append(points, telemetry.GaugePoint{Value: overflow, Attrs: []telemetry.Attr{{Key: semconv.AttrStatusComponentName, Value: "other"}, {Key: semconv.AttrStatusComponentType, Value: "remainder"}}})
	}
	// The SDK starts expiry at publication time, never a historical window bound.
	// A validated empty array publishes an empty generation to clear retired rows.
	if batchOK {
		return batch.GaugeSnapshots(ctx, 3*c.interval, map[string][]telemetry.GaugePoint{semconv.MetricStatusComponentStatus: points})
	}
	return single.GaugeSnapshot(ctx, semconv.MetricStatusComponentStatus, 3*c.interval, points)
}
