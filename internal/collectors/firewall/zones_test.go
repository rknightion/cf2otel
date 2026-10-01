package firewall

import (
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// Keep data metric assertions distinct from the new poll gauges. Error paths
// still inspect the whole buffer to preserve their no-partial-output contract.
func firewallDataMetrics(out *telemetry.Buffer) []telemetry.BufferedMetric {
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
