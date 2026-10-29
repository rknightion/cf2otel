package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/semconv"
)

// NewLogHandler builds the process logger from log.level and log.format.
func NewLogHandler(w io.Writer, level, format string) (slog.Handler, error) {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		return nil, fmt.Errorf("unknown log level %q", level)
	}
	opts := &slog.HandlerOptions{Level: l}
	switch strings.ToLower(format) {
	case "json":
		return slog.NewJSONHandler(w, opts), nil
	case "text":
		return slog.NewTextHandler(w, opts), nil
	}
	return nil, fmt.Errorf("unknown log format %q", format)
}

// NewProcessLogHandler writes each record through next and also emits it as
// one cf2otel.process.log OTLP record. The body is the JSON-encoded record:
// slog keys are free-form, and only internal/semconv declares attribute names.
func NewProcessLogHandler(next slog.Handler, logger otellog.Logger) slog.Handler {
	return newProcessLogHandler(next, logger, true)
}

func newProcessLogHandler(next slog.Handler, logger otellog.Logger, eventName bool) slog.Handler {
	shared := &bodyBuffer{}
	return &processLogHandler{next: next, logger: logger, eventName: eventName, shared: shared, encoder: slog.NewJSONHandler(shared, &slog.HandlerOptions{Level: slog.LevelDebug})}
}

type bodyBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write is called only by the encoder while Handle holds mu.
func (b *bodyBuffer) Write(p []byte) (int, error) { return b.buf.Write(p) }

type processLogHandler struct {
	next      slog.Handler
	logger    otellog.Logger
	eventName bool
	shared    *bodyBuffer
	encoder   slog.Handler
}

func (h *processLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *processLogHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.next.Handle(ctx, r)
	h.shared.mu.Lock()
	h.shared.buf.Reset()
	_ = h.encoder.Handle(ctx, r)
	body := strings.TrimSuffix(h.shared.buf.String(), "\n")
	h.shared.mu.Unlock()
	at := r.Time
	if at.IsZero() {
		at = time.Now()
	}
	var record otellog.Record
	record.SetTimestamp(at)
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(processSeverity(r.Level))
	record.SetSeverityText(r.Level.String())
	record.SetBody(attribute.StringValue(validUTF8(body)))
	if h.eventName {
		record.AddAttributes(attribute.String(semconv.AttrEventName, semconv.EventProcessLog))
	}
	h.logger.Emit(context.WithoutCancel(ctx), record)
	return err
}

func (h *processLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &processLogHandler{next: h.next.WithAttrs(attrs), logger: h.logger, eventName: h.eventName, shared: h.shared, encoder: h.encoder.WithAttrs(attrs)}
}

func (h *processLogHandler) WithGroup(name string) slog.Handler {
	return &processLogHandler{next: h.next.WithGroup(name), logger: h.logger, eventName: h.eventName, shared: h.shared, encoder: h.encoder.WithGroup(name)}
}

func processSeverity(level slog.Level) otellog.Severity {
	switch {
	case level < slog.LevelInfo:
		return otellog.SeverityDebug
	case level < slog.LevelWarn:
		return otellog.SeverityInfo
	case level < slog.LevelError:
		return otellog.SeverityWarn
	default:
		return otellog.SeverityError
	}
}
