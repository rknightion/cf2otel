package collector_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
	colLog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colMetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type sdkWindow struct{ name string }

func (w sdkWindow) Name() string {
	if w.name != "" {
		return w.name
	}
	return "sdk.window"
}
func (sdkWindow) DefaultInterval() time.Duration { return time.Minute }
func (sdkWindow) Lag() time.Duration             { return 0 }
func (sdkWindow) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	if err := e.LogEvent(ctx, "fixture.event", "one", from, otellog.SeverityInfo); err != nil {
		return from, err
	}
	if err := e.Span(ctx, telemetry.SpanSpec{Name: "fixture.span", Start: from, End: to, Kind: trace.SpanKindInternal}); err != nil {
		return from, err
	}
	if err := e.Counter(ctx, "fixture.requests", 1); err != nil {
		return from, err
	}
	return to, nil
}

func decodedLogs(r *http.Request) (int, error) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return 0, err
	}
	var req colLog.ExportLogsServiceRequest
	if err := proto.Unmarshal(b, &req); err != nil {
		return 0, err
	}
	n := 0
	for _, resource := range req.ResourceLogs {
		for _, scope := range resource.ScopeLogs {
			n += len(scope.LogRecords)
		}
	}
	return n, nil
}
func metricValue(r *http.Request, name string) (float64, error) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return 0, err
	}
	var req colMetric.ExportMetricsServiceRequest
	if err := proto.Unmarshal(b, &req); err != nil {
		return 0, err
	}
	for _, resource := range req.ResourceMetrics {
		for _, scope := range resource.ScopeMetrics {
			for _, metric := range scope.Metrics {
				if metric.Name == name {
					for _, point := range metric.GetSum().DataPoints {
						return point.GetAsDouble(), nil
					}
				}
			}
		}
	}
	return 0, nil
}

func TestRealSDK503RecoveryCheckpoint(t *testing.T) {
	var mu sync.Mutex
	failing := true
	acceptedLogs, acceptedSpans := 0, 0
	payloadMetric := float64(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if failing && (r.URL.Path == "/v1/logs" || r.URL.Path == "/v1/traces") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/v1/logs":
			n, err := decodedLogs(r)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			acceptedLogs += n
		case "/v1/traces":
			acceptedSpans++
		case "/v1/metrics":
			v, err := metricValue(r, "fixture.requests")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if v > payloadMetric {
				payloadMetric = v
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	statePath := filepath.Join(t.TempDir(), "state.json")
	store, err := collector.NewFileStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(nil, providers.Emitter, store)
	s.Flusher = providers
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	entry := collector.Entry{Collector: sdkWindow{}, Interval: time.Minute, InitialLookback: time.Minute}
	if err := s.RunOnce(context.Background(), entry); err == nil {
		t.Fatal("503 commit unexpectedly succeeded")
	}
	if mark, ok := store.Get("sdk.window"); !ok || !mark.Equal(time.Date(2026, 9, 23, 11, 59, 0, 0, time.UTC)) {
		t.Fatalf("checkpoint=%s, want initial lower cursor after SDK 503", mark)
	}
	mu.Lock()
	before := payloadMetric
	mu.Unlock()
	if before != 0 {
		t.Fatalf("payload metric before successful export=%v", before)
	}
	mu.Lock()
	failing = false
	mu.Unlock()
	reopened, err := collector.NewFileStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	s = collector.NewScheduler(nil, providers.Emitter, reopened)
	s.Flusher = providers
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Get("sdk.window"); !ok {
		t.Fatal("checkpoint did not advance after recovery")
	}
	mu.Lock()
	logs, spans := acceptedLogs, acceptedSpans
	mu.Unlock()
	if logs != 1 || spans != 1 {
		t.Fatalf("accepted logs=%d spans=%d, want one each", logs, spans)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		value := payloadMetric
		mu.Unlock()
		if value == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	value := payloadMetric
	mu.Unlock()
	t.Fatalf("payload metric=%v, want one increment", value)
}

type manyLogsWindow struct{ n, bodySize int }

func (manyLogsWindow) Name() string                   { return "sdk.many" }
func (manyLogsWindow) DefaultInterval() time.Duration { return time.Minute }
func (manyLogsWindow) Lag() time.Duration             { return 0 }
func (w manyLogsWindow) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	for i := 0; i < w.n; i++ {
		body := strconv.Itoa(i)
		if w.bodySize > 0 {
			body = strings.Repeat("x", w.bodySize)
		}
		if err := e.LogEvent(ctx, "fixture.event", body, from, otellog.SeverityInfo); err != nil {
			return from, err
		}
	}
	return to, nil
}
func TestRealSDKBoundsLargeExportRequests(t *testing.T) {
	const maxBytes = 512 * 1024
	var mu sync.Mutex
	count, maxSeen := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/logs" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			if len(body) > maxSeen {
				maxSeen = len(body)
			}
			mu.Unlock()
			if len(body) > maxBytes {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			var req colLog.ExportLogsServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			n := 0
			for _, resource := range req.ResourceLogs {
				for _, scope := range resource.ScopeLogs {
					n += len(scope.LogRecords)
				}
			}
			mu.Lock()
			count += n
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(nil, providers.Emitter, store)
	s.Flusher = providers
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	entry := collector.Entry{Collector: manyLogsWindow{n: 6, bodySize: 160 * 1024}, Interval: time.Minute, InitialLookback: time.Minute}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got, largest := count, maxSeen
	mu.Unlock()
	if got != 6 || largest > maxBytes {
		t.Fatalf("accepted %d large records, largest request %d bytes", got, largest)
	}
	t.Logf("accepted %d records; largest OTLP request %d bytes", got, largest)
}

type largeContentSpanWindow struct{}

func (largeContentSpanWindow) Name() string                   { return "sdk.large_content" }
func (largeContentSpanWindow) DefaultInterval() time.Duration { return time.Minute }
func (largeContentSpanWindow) Lag() time.Duration             { return 0 }
func (largeContentSpanWindow) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	request := strings.Repeat("r", 160*1024)
	response := strings.Repeat("s", 160*1024)
	content := []telemetry.Attr{{Key: "request", Value: request}, {Key: "response", Value: response}}
	span := telemetry.SpanSpec{
		Name: "fixture.large_content", Start: from, End: to, Kind: trace.SpanKindClient,
		Attrs:  content,
		Events: []telemetry.SpanEvent{{Name: "fixture.content", At: to, Attrs: content}},
		Logs: []telemetry.LogRecord{
			{Name: "fixture.content", Body: request, At: to, Severity: otellog.SeverityInfo},
			{Name: "fixture.content", Body: response, At: to, Severity: otellog.SeverityInfo},
		},
	}
	if err := e.Span(ctx, span); err != nil {
		return from, err
	}
	return to, nil
}

func TestRealSDKExportsValidTwoSidedContentSpan(t *testing.T) {
	const maxRequestBytes = 1024 * 1024
	var mu sync.Mutex
	var logs, spans, largest int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		if len(body) > largest {
			largest = len(body)
		}
		if r.URL.Path == "/v1/traces" {
			spans++
		}
		if r.URL.Path == "/v1/logs" {
			var request colLog.ExportLogsServiceRequest
			if err := proto.Unmarshal(body, &request); err != nil {
				mu.Unlock()
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, resource := range request.ResourceLogs {
				for _, scope := range resource.ScopeLogs {
					logs += len(scope.LogRecords)
				}
			}
		}
		mu.Unlock()
		if len(body) > maxRequestBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(nil, providers.Emitter, store)
	s.Flusher = providers
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	if err := s.RunOnce(context.Background(), collector.Entry{Collector: largeContentSpanWindow{}, InitialLookback: time.Minute}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotLogs, gotSpans, maxSeen := logs, spans, largest
	mu.Unlock()
	if gotLogs != 2 || gotSpans != 1 || maxSeen > maxRequestBytes {
		t.Fatalf("log batches=%d span batches=%d largest=%d", gotLogs, gotSpans, maxSeen)
	}
}
func TestRealSDKCredentialStallAndPayloadDrop(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		logStatus, traceStatus int
		drop                   bool
	}{
		{"unauthorized", http.StatusUnauthorized, http.StatusUnauthorized, false},
		{"bad_request", http.StatusBadRequest, http.StatusBadRequest, true},
		{"mixed_400_401", http.StatusBadRequest, http.StatusUnauthorized, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/logs" || r.URL.Path == "/v1/traces" {
					status := tc.logStatus
					if r.URL.Path == "/v1/traces" {
						status = tc.traceStatus
					}
					w.WriteHeader(status)
					if status == http.StatusUnauthorized {
						_, _ = w.Write([]byte("fake 400 Bad Request in response body"))
					}
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = providers.Shutdown(ctx)
			}()
			store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			s := collector.NewScheduler(nil, providers.Emitter, store)
			s.Flusher = providers
			s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
			entry := collector.Entry{Collector: sdkWindow{}, Interval: time.Minute, InitialLookback: time.Minute}
			for i := 0; i < 3; i++ {
				runErr := s.RunOnce(context.Background(), entry)
				if runErr == nil && (i < 2 || !tc.drop) {
					t.Fatalf("commit %d unexpectedly succeeded", i+1)
				}
				if runErr != nil && i == 2 && tc.drop {
					t.Fatalf("third payload rejection did not drop: %v", runErr)
				}
			}
			mark, present := store.Get("sdk.window")
			want := time.Date(2026, 9, 23, 11, 59, 0, 0, time.UTC)
			if tc.drop {
				want = want.Add(time.Minute)
			}
			if !present || !mark.Equal(want) {
				t.Fatalf("checkpoint=%s present=%v, want %s", mark, present, want)
			}
		})
	}
}
func TestRealSDKExportsMoreThanQueueCapacity(t *testing.T) {
	var mu sync.Mutex
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/logs" {
			time.Sleep(25 * time.Millisecond)
			n, err := decodedLogs(r)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			count += n
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(nil, providers.Emitter, store)
	s.Flusher = providers
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	entry := collector.Entry{Collector: manyLogsWindow{n: 2050}, Interval: time.Minute, InitialLookback: time.Minute}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := count
	mu.Unlock()
	if got != 2050 {
		t.Fatalf("accepted %d logs, want 2050", got)
	}
	t.Logf("accepted %d logs across chunked exports", got)
	if _, ok := store.Get("sdk.many"); !ok {
		t.Fatal("checkpoint did not advance")
	}
}

func TestRealSDKPartialFailureRetriesWithoutCheckpoint(t *testing.T) {
	var mu sync.Mutex
	acceptedSpans := 0
	retryMetric := float64(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/logs":
			w.WriteHeader(http.StatusBadRequest)
			return
		case "/v1/traces":
			mu.Lock()
			acceptedSpans++
			mu.Unlock()
		case "/v1/metrics":
			value, err := metricValue(r, "cf2otel.window.commit_failures")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			if value > retryMetric {
				retryMetric = value
			}
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(nil, providers.Emitter, store)
	s.Flusher = providers
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	entry := collector.Entry{Collector: sdkWindow{}, Interval: time.Minute, InitialLookback: time.Minute}
	if err := s.RunOnce(context.Background(), entry); err == nil {
		t.Fatal("partial failure unexpectedly committed")
	}
	if mark, ok := store.Get("sdk.window"); !ok || !mark.Equal(time.Date(2026, 9, 23, 11, 59, 0, 0, time.UTC)) {
		t.Fatalf("checkpoint=%s, want initial lower cursor after partial failure", mark)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		value := retryMetric
		mu.Unlock()
		if value == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	spans, value := acceptedSpans, retryMetric
	mu.Unlock()
	if spans != 1 || value != 1 {
		t.Fatalf("accepted spans=%d retry metric=%v, want one each", spans, value)
	}
}

func TestRealSDKConcurrentFlushIsolation(t *testing.T) {
	var mu sync.Mutex
	logRequests := 0
	firstLog := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/logs" {
			mu.Lock()
			logRequests++
			first := logRequests == 1
			mu.Unlock()
			if first {
				close(firstLog)
				<-release
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(nil, providers.Emitter, store)
	s.Flusher = providers
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	a := collector.Entry{Collector: sdkWindow{name: "first"}, Interval: time.Minute, InitialLookback: time.Minute}
	b := collector.Entry{Collector: sdkWindow{name: "second"}, Interval: time.Minute, InitialLookback: time.Minute}
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- s.RunOnce(context.Background(), a) }()
	select {
	case <-firstLog:
	case <-time.After(5 * time.Second):
		t.Fatal("first log export did not start")
	}
	go func() { secondDone <- s.RunOnce(context.Background(), b) }()
	select {
	case <-secondDone:
		t.Fatal("second commit escaped blocked export")
	case <-time.After(30 * time.Millisecond):
	}
	mu.Lock()
	requests := logRequests
	mu.Unlock()
	if requests != 1 {
		t.Fatalf("log requests before release=%d, want one", requests)
	}
	close(release)
	if err := <-firstDone; err == nil {
		t.Fatal("first commit unexpectedly succeeded")
	}
	if err := <-secondDone; err == nil {
		t.Fatal("second commit unexpectedly succeeded")
	}
	if mark, ok := store.Get("first"); !ok || !mark.Equal(time.Date(2026, 9, 23, 11, 59, 0, 0, time.UTC)) {
		t.Fatalf("first checkpoint=%s, want initial lower cursor", mark)
	}
	if mark, ok := store.Get("second"); !ok || !mark.Equal(time.Date(2026, 9, 23, 11, 59, 0, 0, time.UTC)) {
		t.Fatalf("second checkpoint=%s, want initial lower cursor", mark)
	}
}
