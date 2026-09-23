package telemetry

import (
	"context"
	"encoding/base64"
	"errors"
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
}
type Providers struct {
	Emitter Emitter
	metrics *sdkmetric.MeterProvider
	logs    *sdklog.LoggerProvider
	traces  *sdktrace.TracerProvider
	hook    *exportObserver
}

func NewProviders(ctx context.Context, o ProviderOptions) (*Providers, error) {
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
		lx, err = otlploghttp.New(ctx, otlploghttp.WithEndpointURL(base+"/v1/logs"), otlploghttp.WithHeaders(h))
		if err != nil {
			return nil, err
		}
		tx, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(base+"/v1/traces"), otlptracehttp.WithHeaders(h))
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
		lx, err = otlploggrpc.New(ctx, otlploggrpc.WithEndpoint(endpoint), otlploggrpc.WithHeaders(h))
		if err != nil {
			return nil, err
		}
		tx, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithHeaders(h))
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
	mx = observedMetricExporter{Exporter: mx, hook: hook}
	lx = observedLogExporter{Exporter: lx, hook: hook}
	tx = observedTraceExporter{SpanExporter: tx, hook: hook}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mx, sdkmetric.WithInterval(o.Interval))))
	// The log SDK's batch queue drops records on overflow without reporting the
	// loss through ForceFlush. A window checkpoint therefore needs synchronous
	// log export. Trace batching can retain backpressure instead of dropping.
	lp := sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewSimpleProcessor(lx)))
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(tx, sdktrace.WithBlocking()))
	return &Providers{Emitter: NewEmitter(mp.Meter(semconv.ServiceName), lp.Logger(semconv.ServiceName), tp.Tracer(semconv.ServiceName)), metrics: mp, logs: lp, traces: tp, hook: hook}, nil
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
