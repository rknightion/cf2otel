package r2

import "sort"

// Cap complete attribute sets, not just buckets: two actions on one bucket are
// two series. The reserved remainder combines counters by SUM and gauges by MAX.
func (c *datasetCollector) capResources(points map[r2SeriesKey]*r2MetricPoint) map[r2SeriesKey]*r2MetricPoint {
	c.admissionMu.Lock()
	defer c.admissionMu.Unlock()
	if c.admitted == nil {
		c.admitted = map[r2SeriesKey]bool{}
	}
	counts := map[string]int{}
	for key := range c.admitted {
		counts[key.metric]++
	}
	keys := make([]r2SeriesKey, 0, len(points))
	for key := range points {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].metric != keys[j].metric {
			return keys[i].metric < keys[j].metric
		}
		if keys[i].resource != keys[j].resource {
			return keys[i].resource < keys[j].resource
		}
		return keys[i].action < keys[j].action
	})
	result := map[r2SeriesKey]*r2MetricPoint{}
	for _, key := range keys {
		point := *points[key]
		// An absent optional dimension stays absent. If every dimension is absent,
		// this is the safe account aggregate and needs no resource admission.
		if key.resource != "" || key.action != "" {
			remainder := key.resource == "other" || key.action == "other"
			if !remainder && !c.admitted[key] {
				if counts[key.metric] < 49 {
					c.admitted[key] = true
					counts[key.metric]++
				} else {
					remainder = true
				}
			}
			if remainder {
				key.resource = "other"
				point.resource = "other"
				if c.spec.collector == "r2.operations" {
					key.action = "other"
					point.action = "other"
				}
			}
		} else {
			key.resource = "other"
			point.resource = "other"
			if c.spec.collector == "r2.operations" {
				key.action = "other"
				point.action = "other"
			}
		}
		if previous := result[key]; previous != nil {
			if point.gauge {
				previous.value = max(previous.value, point.value)
			} else {
				previous.value += point.value
			}
		} else {
			result[key] = &point
		}
	}
	return result
}
