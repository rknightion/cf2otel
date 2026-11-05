package telemetry

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
)

// PollSummary distinguishes checkpointed delivery from attempted collection.
// Request time sums overlap concurrent work; they are not wall-time phases.
type PollSummary struct {
	Collector         string              `json:"collector"`
	Outcome           string              `json:"outcome"`
	DurationSeconds   float64             `json:"duration_seconds"`
	CollectSeconds    float64             `json:"collect_seconds"`
	CommitLockSeconds float64             `json:"commit_lock_seconds"`
	CommitSeconds     float64             `json:"commit_seconds"`
	CommittedWindows  int64               `json:"committed_windows"`
	CommittedLogs     int64               `json:"committed_logs"`
	CommittedSpans    int64               `json:"committed_spans"`
	Requests          cfapi.RequestTotals `json:"api"`
}
type operationalContextKey struct{}

func operationalContext(ctx context.Context) context.Context {
	if op, ok := ctx.Value(operationalContextKey{}).(context.Context); ok {
		return op
	}
	return ctx
}

// ObservePoll keeps the operational parent separate from imported source traces.
// Summary logs use the process pipeline, outside checkpoint delivery accounting.
func (p *Providers) ObservePoll(ctx context.Context, name string) (context.Context, func(PollSummary)) {
	op, span := p.operations.Tracer("cf2otel.poll").Start(ctx, semconv.SpanPoll, trace.WithSpanKind(trace.SpanKindInternal), trace.WithAttributes(attrs(p.policy.attrs([]Attr{{Key: semconv.AttrCollector, Value: name}}))...))
	op = context.WithValue(op, operationalEmitterKey{}, filterEmitterWithPolicy(NewEmitter(nil, nil, p.operations.Tracer("cf2otel.api")), p.policy))
	return context.WithValue(ctx, operationalContextKey{}, op), func(s PollSummary) {
		defer span.End()
		span.SetAttributes(attrs(p.policy.attrs([]Attr{{Key: semconv.AttrPollOutcome, Value: s.Outcome}}))...)
		if s.Outcome == "error" {
			span.SetStatus(codes.Error, "collector failed")
		}
		body, _ := json.Marshal(s)
		var record otellog.Record
		record.SetTimestamp(time.Now())
		record.SetObservedTimestamp(time.Now())
		record.SetSeverity(otellog.SeverityInfo)
		if s.Outcome == "error" {
			record.SetSeverity(otellog.SeverityError)
		}
		record.SetBody(attribute.StringValue(string(body)))
		record.AddAttributes(attrs(p.policy.attrs([]Attr{{Key: semconv.AttrEventName, Value: semconv.EventPoll}, {Key: semconv.AttrCollector, Value: name}, {Key: semconv.AttrPollOutcome, Value: s.Outcome}}))...)
		p.process.Logger("cf2otel.poll").Emit(context.WithoutCancel(op), record)
		a := []Attr{{Key: semconv.AttrCollector, Value: name}}
		for phase, value := range map[string]float64{"collect": s.CollectSeconds, "commit_lock": s.CommitLockSeconds, "commit": s.CommitSeconds} {
			_ = p.Emitter.Histogram(context.WithoutCancel(op), semconv.MetricPollPhaseDuration, value, append(a, Attr{Key: semconv.AttrPollPhase, Value: phase})...)
		}
		for signal, value := range map[string]int64{"windows": s.CommittedWindows, "logs": s.CommittedLogs, "spans": s.CommittedSpans} {
			_ = p.Emitter.Counter(context.WithoutCancel(op), semconv.MetricPollCommitted, float64(value), append(a, Attr{Key: semconv.AttrExportSignal, Value: signal})...)
		}
	}
}

type operationalEmitterKey struct{}
