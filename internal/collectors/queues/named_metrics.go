package queues

import (
	"context"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func (c *groupsCollector) aggregateNamed(ctx context.Context, rows []map[string]any, from, to time.Time, fields []string) ([]metricValue, error) {
	selected := false
	for _, field := range fields {
		if field == "dimensions."+resourceDimension {
			selected = true
		}
	}

	// Validate the complete source before changing lifetime admission state.
	if _, err := c.aggregate(rows, from, to); err != nil {
		return nil, err
	}
	var names map[string]string
	if selected && len(rows) > 0 {
		names = c.names.lookup(ctx, c.api, c.cfg.Cloudflare.AccountID)
	}
	groups := namedRows(rows, names, from, to, c.spec.metrics[0].kind == gaugeMetric)
	var points []metricValue
	for _, name := range sortedNames(groups) {
		values, err := c.aggregate(groups[name], from, to)
		if err != nil {
			return nil, err
		}
		for _, point := range values {
			point.attrs = []telemetry.Attr{{Key: semconv.AttrQueuesQueueName, Value: name}}
			points = append(points, point)
		}
	}
	type series struct{ metric, name string }
	merged := map[series]int{}
	var result []metricValue
	for _, point := range points {
		name := c.admission.admit(point.name, point.attrs[0].Value)
		point.attrs[0].Value = name
		key := series{point.name, name}
		if index, ok := merged[key]; ok {
			if point.kind == counterMetric {
				result[index].value += point.value
			} else {
				result[index].value = max(result[index].value, point.value)
			}
		} else {
			merged[key] = len(result)
			result = append(result, point)
		}
	}
	return result, nil
}
