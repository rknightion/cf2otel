package telemetry

import (
	"context"
	"testing"

	"github.com/rknightion/cf2otel/internal/semconv"
)

func TestDenylistDirectCardinalityPath(t *testing.T) {
	for _, scenario := range []string{"metric", "attribute"} {
		t.Run(scenario, func(t *testing.T) {
			p, c := testCardinalityProvider(t, 3, func(o *ProviderOptions) {
				if scenario == "metric" {
					denyOptions(o, []string{semconv.MetricCardinalityOverflows}, nil)
				} else {
					denyOptions(o, nil, []string{semconv.AttrInstrument})
				}
			})
			for _, id := range []string{"opaque-a", "opaque-b", "opaque-c", "opaque-d"} {
				if err := p.Emitter.Counter(context.Background(), semconv.MetricDNSQueries, 1, Attr{Key: semconv.AttrDNSZone, Value: id}); err != nil {
					t.Fatal(err)
				}
			}
			flushCardinality(t, p)
			flushCardinality(t, p)
			if c.find(semconv.MetricDNSQueries) == nil {
				t.Fatal("allowed counter missing")
			}
			m := c.find(semconv.MetricCardinalityOverflows)
			if scenario == "metric" {
				if m != nil {
					t.Fatal("denied direct SDK counter exported")
				}
				return
			}
			if m == nil {
				t.Fatal("allowed overflow counter missing")
			}
			for _, dp := range m.GetSum().DataPoints {
				for _, attr := range dp.Attributes {
					if attr.Key == semconv.AttrInstrument {
						t.Fatal("denied direct SDK attribute exported")
					}
				}
			}
		})
	}
}
