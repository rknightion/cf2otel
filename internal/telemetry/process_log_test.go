package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/rknightion/cf2otel/internal/semconv"
)

func TestProcessLogsTeeToOTLPAtTheConfiguredLevel(t *testing.T) {
	var stderr bytes.Buffer
	base, err := NewLogHandler(&stderr, "warn", "json")
	if err != nil {
		t.Fatal(err)
	}
	records := &recordExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(records)))
	logger := slog.New(NewProcessLogHandler(base, lp.Logger(semconv.ServiceName))).With("collector", "aigateway.logs")
	logger.Info("below the level")
	logger.Error("collector run failed", "error", errors.New("fixture failure"))

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("stderr lines = %q", lines)
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil || line["msg"] != "collector run failed" || line["collector"] != "aigateway.logs" {
		t.Fatalf("stderr is not the JSON record: %q (%v)", lines[0], err)
	}
	if len(records.records) != 1 {
		t.Fatalf("exported %d records, want 1", len(records.records))
	}
	r := records.records[0]
	if r.Severity() != otellog.SeverityError || r.SeverityText() != "ERROR" {
		t.Fatalf("severity = %v %q", r.Severity(), r.SeverityText())
	}
	body := r.Body().AsString()
	if !strings.Contains(body, "collector run failed") || !strings.Contains(body, "fixture failure") || !strings.Contains(body, "aigateway.logs") {
		t.Fatalf("body lost the record: %q", body)
	}
	event := ""
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		if kv.Key == semconv.AttrEventName {
			event = kv.Value.AsString()
		}
		return true
	})
	if event != semconv.EventProcessLog {
		t.Fatalf("event_name = %q", event)
	}
}

func TestLogHandlerRejectsUnknownSettings(t *testing.T) {
	if _, err := NewLogHandler(&bytes.Buffer{}, "verbose", "json"); err == nil {
		t.Fatal("unknown level accepted")
	}
	if _, err := NewLogHandler(&bytes.Buffer{}, "info", "logfmt"); err == nil {
		t.Fatal("unknown format accepted")
	}
	h, err := NewLogHandler(&bytes.Buffer{}, "debug", "text")
	if err != nil || !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatalf("debug text handler: %v", err)
	}
}
