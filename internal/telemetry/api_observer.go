package telemetry

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
)

// APIRequestObserver emits physical-request metrics directly, independent of
// collector buffering and checkpoint success. The metric dimensions are only
// the scheduler name, REST/GraphQL kind and a failure-only bounded error class.
// Existing client spans and retry counters retain their status-class dimensions.
func APIRequestObserver(emitter Emitter) cfapi.RequestObserver {
	return func(ctx context.Context, observation cfapi.RequestObservation) {
		attrs := []Attr{
			{Key: semconv.AttrCollector, Value: cfapi.RequestCollector(ctx)},
			{Key: semconv.AttrAPIMethod, Value: observation.Method},
		}
		if observation.ErrorClass != "" {
			attrs = append(attrs, Attr{Key: semconv.AttrErrorClass, Value: observation.ErrorClass})
		}
		_ = emitter.Counter(ctx, semconv.MetricAPIRequests, 1, attrs...)
		_ = emitter.Histogram(ctx, semconv.MetricAPIDuration, observation.Duration.Seconds(), attrs...)
		_ = emitter.Histogram(ctx, semconv.MetricAPILimiterWait, observation.LimiterWait.Seconds(), attrs...)

		// Preserve the existing span and retry signal contract without leaking
		// paths, query strings, response text or identifiers into either signal.
		statusAttrs := []Attr{{Key: semconv.AttrStatusClass, Value: fmt.Sprintf("%dxx", observation.Status/100)}}
		verb := http.MethodGet
		if observation.Method == "graphql" {
			verb = http.MethodPost
		}
		_ = emitter.Span(ctx, SpanSpec{Name: semconv.SpanAPIRequest, Start: observation.Started,
			End: observation.Started.Add(observation.Duration), Kind: trace.SpanKindClient,
			Attrs: append(statusAttrs, Attr{Key: semconv.AttrAPIMethod, Value: verb})})
		if observation.Status == 429 || observation.Status == 502 || observation.Status == 503 || observation.Status == 504 {
			_ = emitter.Counter(ctx, semconv.MetricAPIRetries, 1, statusAttrs...)
		}
	}
}
