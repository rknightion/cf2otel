package collector_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	colLog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/proto"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type catchupSDKWindow struct{}

func (catchupSDKWindow) Name() string                   { return "sdk.catchup" }
func (catchupSDKWindow) DefaultInterval() time.Duration { return 5 * time.Minute }
func (catchupSDKWindow) Lag() time.Duration             { return 0 }
func (catchupSDKWindow) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	if err := e.LogEvent(ctx, "fixture.event", from.Format(time.RFC3339), from, otellog.SeverityInfo); err != nil {
		return from, err
	}
	return to, nil
}

func TestCatchupOTLPSecondWindowFailureRecovery(t *testing.T) {
	end := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	failingBody := end.Add(-45 * time.Minute).Format(time.RFC3339)
	var mu sync.Mutex
	failSecond := true
	accepted := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/logs" {
			w.WriteHeader(http.StatusOK)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var req colLog.ExportLogsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, resource := range req.ResourceLogs {
			for _, scope := range resource.ScopeLogs {
				for _, record := range scope.LogRecords {
					value := record.Body.GetStringValue()
					if failSecond && value == failingBody {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					accepted[value]++
				}
			}
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
	if err := store.Set("sdk.catchup", end.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(nil, providers.Emitter, store)
	s.Flusher = providers
	s.Now = func() time.Time { return end }
	entry := collector.Entry{Collector: catchupSDKWindow{}, Interval: 5 * time.Minute, MaxWindow: 15 * time.Minute}
	if err := s.RunOnce(context.Background(), entry); err == nil {
		t.Fatal("second OTLP export should fail")
	}
	mark, _ := store.Get("sdk.catchup")
	if !mark.Equal(end.Add(-45 * time.Minute)) {
		t.Fatalf("checkpoint after failure=%s", mark)
	}
	mu.Lock()
	failSecond = false
	mu.Unlock()
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	mark, _ = store.Get("sdk.catchup")
	if !mark.Equal(end) {
		t.Fatalf("recovered checkpoint=%s", mark)
	}
	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < 4; i++ {
		key := end.Add(-time.Hour + time.Duration(i)*15*time.Minute).Format(time.RFC3339)
		if accepted[key] != 1 {
			t.Fatalf("accepted %s %d times, want once", key, accepted[key])
		}
	}
}
