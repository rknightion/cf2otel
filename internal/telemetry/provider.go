package telemetry

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/rknightion/cf2otel/internal/semconv"
)

type ProviderOptions struct {
	Endpoint, Protocol, InstanceID, Token, ServiceVersion, InstanceUUID string
	Headers                                                             map[string]string
	Interval                                                            time.Duration
	// CardinalityLimit is per instrument: zero preserves the SDK default,
	// negative disables the limit, and positive sets the global SDK limit.
	CardinalityLimit                  int
	MetricDenylist, AttributeDenylist []string
	// PrometheusReader adds pull export alongside the unchanged OTLP reader.
	// Once registered, its shutdown is owned by this provider.
	PrometheusReader sdkmetric.Reader
}
type Providers struct {
	Emitter Emitter
	metrics *sdkmetric.MeterProvider
	logs    *sdklog.LoggerProvider
	traces  *sdktrace.TracerProvider
	hook    *exportObserver
}

func NewProviders(ctx context.Context, o ProviderOptions) (*Providers, error) {
	readerOwned := o.PrometheusReader != nil
	defer func() {
		if readerOwned {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = o.PrometheusReader.Shutdown(cleanup)
		}
	}()
	for _, name := range o.MetricDenylist {
		if _, ok := semconv.Metric(name); !ok {
			return nil, fmt.Errorf("unknown metric deny key %q", name)
		}
	}
	for _, name := range o.AttributeDenylist {
		if !semconv.IsAttribute(name) {
			return nil, fmt.Errorf("unknown attribute deny key %q", name)
		}
	}
	policy := newDenyPolicy(o.MetricDenylist, o.AttributeDenylist)
	if o.Endpoint == "" {
		return nil, errors.New("OTLP endpoint required")
	}
	h := map[string]string{}
	for k, v := range o.Headers {
		h[k] = v
	}
	if o.InstanceID != "" && o.Token != "" {
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(o.InstanceID+":"+o.Token))
	}
	base := strings.TrimRight(o.Endpoint, "/")
	var mx sdkmetric.Exporter
	var lx sdklog.Exporter
	var tx sdktrace.SpanExporter
	var err error
	switch o.Protocol {
	case "", "http":
		u, parseErr := url.ParseRequestURI(base)
		if parseErr != nil {
			return nil, parseErr
		}
		if len(h) > 0 && u.Scheme != "https" {
			return nil, errors.New("credential-bearing OTLP endpoint must use https")
		}
		if u.Host == "" {
			return nil, errors.New("OTLP endpoint requires a host")
		}
		mx, err = otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(base+"/v1/metrics"), otlpmetrichttp.WithHeaders(h))
		if err != nil {
			return nil, err
		}
		lx, err = otlploghttp.New(ctx, otlploghttp.WithEndpointURL(base+"/v1/logs"), otlploghttp.WithHeaders(h), otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: false}))
		if err != nil {
			return nil, err
		}
		tx, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(base+"/v1/traces"), otlptracehttp.WithHeaders(h), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))
		if err != nil {
			return nil, err
		}
	case "grpc":
		u, parseErr := url.Parse(base)
		if parseErr != nil || u.Host == "" || u.Scheme != "https" {
			return nil, errors.New("gRPC endpoint must be an https URL")
		}
		endpoint := u.Host
		mx, err = otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint(endpoint), otlpmetricgrpc.WithHeaders(h))
		if err != nil {
			return nil, err
		}
		lx, err = otlploggrpc.New(ctx, otlploggrpc.WithEndpoint(endpoint), otlploggrpc.WithHeaders(h), otlploggrpc.WithRetry(otlploggrpc.RetryConfig{Enabled: false}))
		if err != nil {
			return nil, err
		}
		tx, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithHeaders(h), otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: false}))
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unsupported OTLP protocol")
	}
	res, err := resource.New(ctx, resource.WithAttributes(attribute.String(semconv.AttrServiceName, semconv.ServiceName), attribute.String(semconv.AttrServiceVersion, o.ServiceVersion), attribute.String(semconv.AttrServiceInstanceID, o.InstanceUUID)))
	if err != nil {
		return nil, err
	}
	if o.Interval <= 0 {
		o.Interval = 15 * time.Second
	}
	hook := &exportObserver{}
	overflow := &cardinalityExporter{Exporter: mx, lastWarning: make(map[string]time.Time), omitInstrument: policy.attribute(semconv.AttrInstrument)}
	mx = observedMetricExporter{Exporter: overflow, hook: hook}
	lx = observedLogExporter{Exporter: lx, hook: hook}
	lx = newBoundedLogExporter(lx)
	tx = observedTraceExporter{SpanExporter: tx, hook: hook}
	metricOptions := []sdkmetric.Option{sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mx, sdkmetric.WithInterval(o.Interval)))}
	if o.PrometheusReader != nil {
		metricOptions = append(metricOptions, sdkmetric.WithReader(o.PrometheusReader))
	}
	if o.CardinalityLimit != 0 {
		metricOptions = append(metricOptions, sdkmetric.WithCardinalityLimit(o.CardinalityLimit))
	}
	mp := sdkmetric.NewMeterProvider(metricOptions...)
	readerOwned = false // The SDK now owns reader shutdown, including errors below.
	spec, _ := semconv.Metric(semconv.MetricCardinalityOverflows)
	if !policy.metric(semconv.MetricCardinalityOverflows) {
		counter, err := mp.Meter(semconv.ServiceName).Int64Counter(semconv.MetricCardinalityOverflows, metric.WithUnit(spec.Unit), metric.WithDescription(spec.Description))
		if err != nil {
			return nil, errors.Join(err, mp.Shutdown(ctx), lx.Shutdown(ctx), tx.Shutdown(ctx))
		}
		overflow.setCounter(counter)
	}
	// The log SDK's batch queue drops records on overflow without reporting the
	// loss through ForceFlush. Synchronous bounded export retains backpressure.
	lp := sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewSimpleProcessor(lx)))
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(tx, sdktrace.WithBlocking()))
	emitter := NewEmitter(mp.Meter(semconv.ServiceName), lp.Logger(semconv.ServiceName), tp.Tracer(semconv.ServiceName)).(*otelEmitter)
	return &Providers{Emitter: filterEmitterWithPolicy(emitter, policy), metrics: mp, logs: lp, traces: tp, hook: hook}, nil
}
func (p *Providers) SetExportObserver(fn func(context.Context, string, error)) {
	p.hook.Set(fn)
}

// BeginCommit captures export failures that can occur before ForceFlush returns.
func (p *Providers) BeginCommit() uint64 { return p.hook.snapshot() }
func (p *Providers) FlushCommit(ctx context.Context, since uint64) error {
	logErr := p.logs.ForceFlush(ctx)
	traceErr := p.traces.ForceFlush(ctx)
	var failures []error
	if logErr != nil {
		failures = append(failures, &ExportFailure{Signal: "logs", Err: logErr})
	}
	if traceErr != nil {
		failures = append(failures, &ExportFailure{Signal: "traces", Err: traceErr})
	}
	if backgroundErr := p.hook.failedSince(since); backgroundErr != nil {
		failures = append(failures, backgroundErr)
	}
	return errors.Join(failures...)
}
func (p *Providers) Shutdown(ctx context.Context) error {
	return errors.Join(p.metrics.Shutdown(ctx), p.logs.Shutdown(ctx), p.traces.Shutdown(ctx))
}
