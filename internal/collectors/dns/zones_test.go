package dns

import (
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// Existing data assertions exclude only the four newly introduced poll gauges.
// Failed-window assertions continue to inspect the entire unfiltered buffer.
func dnsDataMetrics(out *telemetry.Buffer) []telemetry.BufferedMetric {
	var data []telemetry.BufferedMetric
	for _, m := range out.Metrics {
		switch m.Name {
		case semconv.MetricZonesDiscovered, semconv.MetricZonesFiltered, semconv.MetricZonesProcessed, semconv.MetricZonesSkipped:
			continue
		}
		data = append(data, m)
	}
	return data
}
