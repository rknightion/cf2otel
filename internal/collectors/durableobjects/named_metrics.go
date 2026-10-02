package durableobjects

import (
	"context"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func (c *datasetCollector) aggregateNamed(ctx context.Context, rows []map[string]any, from, to time.Time, fields []string) ([]metricPoint, error) {
	_, _, err := c.aggregate(rows, from, to)
	if err != nil {
		return nil, err
	}
	selected := false
	for _, field := range fields {
		if field == "dimensions."+resourceDimension {
			selected = true
		}
	}
	var names map[string]string
	if selected && len(rows) > 0 {
		names = c.names.lookup(ctx, c.api, c.cfg.Cloudflare.AccountID)
	}
	groups := namedRows(rows, names, from, to, c.spec.kind == gaugeMetric)
	var points []metricPoint
	for _, name := range sortedNames(groups) {
		value, found, err := c.aggregate(groups[name], from, to)
		if err != nil {
			return nil, err
		}
		if found {
			points = append(points, metricPoint{name: c.spec.metric, kind: c.spec.kind, value: value, attrs: []telemetry.Attr{{Key: semconv.AttrDurableObjectsNamespaceName, Value: name}}})
		}
	}
	merged := map[string]int{}
	var result []metricPoint
	for _, point := range points {
		name := c.admission.admit(point.name, point.attrs[0].Value)
		point.attrs[0].Value = name
		if index, ok := merged[name]; ok {
			if point.kind == counterMetric {
				result[index].value += point.value
			} else {
				result[index].value = max(result[index].value, point.value)
			}
		} else {
			merged[name] = len(result)
			result = append(result, point)
		}
	}
	return result, nil
}
