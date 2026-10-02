package r2

import (
	"fmt"
	"testing"
)

func TestStickyCompleteSetCapConservesActionsAcrossWindows(t *testing.T) {
	c := &datasetCollector{spec: r2DatasetSpec{collector: "r2.operations"}}
	points := map[r2SeriesKey]*r2MetricPoint{}
	for i := range 55 {
		action := fmt.Sprintf("Action%02d", i)
		key := r2SeriesKey{metric: "metric-fixture", resource: "named-bucket", action: action}
		points[key] = &r2MetricPoint{name: key.metric, resource: key.resource, action: key.action, value: float64(i + 1)}
	}
	capped := c.capResources(points)
	if len(capped) != 50 {
		t.Fatalf("complete attribute sets = %d, want 49 plus remainder", len(capped))
	}
	total := 0.0
	for _, point := range capped {
		total += point.value
	}
	if total != 1540 {
		t.Fatalf("counter conservation = %v, want 1540", total)
	}
	remainder := r2SeriesKey{metric: "metric-fixture", resource: "other", action: "other"}
	if point := capped[remainder]; point == nil || point.value != 315 {
		t.Fatalf("canonical action remainder = %v, want 315", point)
	}
	// Different action on the same bucket is a new complete set, not a bucket hit.
	newcomer := r2SeriesKey{metric: "metric-fixture", resource: "named-bucket", action: "NewAction"}
	existing := r2SeriesKey{metric: "metric-fixture", resource: "named-bucket", action: "Action00"}
	next := c.capResources(map[r2SeriesKey]*r2MetricPoint{
		newcomer: {name: newcomer.metric, resource: newcomer.resource, action: newcomer.action, value: 7},
		existing: {name: existing.metric, resource: existing.resource, action: existing.action, value: 3},
	})
	if next[newcomer] != nil || next[remainder] == nil || next[remainder].value != 7 || next[existing] == nil || next[existing].value != 3 {
		t.Fatalf("sticky cross-window action admission = %v", next)
	}
	gauges := c.capResources(map[r2SeriesKey]*r2MetricPoint{
		newcomer: {name: newcomer.metric, resource: newcomer.resource, action: newcomer.action, value: 7, gauge: true},
		{metric: "metric-fixture", resource: "other", action: "other"}: {name: "metric-fixture", resource: "other", action: "other", value: 11, gauge: true},
	})
	if len(gauges) != 1 || gauges[remainder].value != 11 {
		t.Fatalf("gauge remainder must be max: %v", gauges)
	}
}
