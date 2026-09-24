package access

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/identity"
)

type replayOnceFlusher struct{ calls int }

func (f *replayOnceFlusher) BeginCommit() uint64 { return uint64(f.calls) }
func (f *replayOnceFlusher) FlushCommit(context.Context, uint64) error {
	f.calls++
	if f.calls == 1 {
		return errors.New("synthetic export failure")
	}
	return nil
}

func TestSchedulerRetryDeduplicatesAccessIdentityObservation(t *testing.T) {
	at := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	rows := []loginRow{
		{RayID: "ray-shared", CreatedAt: at.Add(time.Second), AppDomain: "app.example.test", UserEmail: "a@example.test", IPAddress: "192.0.2.1", Allowed: true},
		{RayID: "ray-distinct", CreatedAt: at.Add(time.Second), AppDomain: "app.example.test", UserEmail: "b@example.test", IPAddress: "192.0.2.2", Allowed: true},
		{RayID: "ray-shared", CreatedAt: at.Add(2 * time.Second), AppDomain: "app.example.test", UserEmail: "c@example.test", IPAddress: "192.0.2.3", Allowed: true},
	}
	api := &loginAPI{rows: rows}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "test-account"
	idx, err := identity.New(10*time.Minute, 6)
	if err != nil {
		t.Fatal(err)
	}
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	flusher := &replayOnceFlusher{}
	now := at.Add(2 * time.Minute)
	entry := collector.Entry{Collector: newLogins(collector.Deps{Config: &cfg, API: api, Identity: idx}), Interval: time.Minute, InitialLookback: time.Minute}
	scheduler := collector.NewScheduler(collector.NewRegistry(), &loginEmitter{}, store)
	scheduler.Flusher = flusher
	scheduler.Now = func() time.Time { return now }

	if err := scheduler.RunOnce(context.Background(), entry); err == nil || err.Error() != "synthetic export failure" {
		t.Fatalf("first scheduler run error = %v, want synthetic export failure", err)
	}
	if got := idx.Len(); got != len(rows) {
		t.Fatalf("first run retained %d identity candidates, want %d", got, len(rows))
	}
	if err := scheduler.RunOnce(context.Background(), entry); err != nil {
		t.Fatalf("scheduler retry: %v", err)
	}
	if flusher.calls != 2 {
		t.Fatalf("flush calls = %d, want one failed flush and one successful flush", flusher.calls)
	}
	var windows [][2]string
	for _, query := range api.queries {
		if query.Get("page") == "1" {
			windows = append(windows, [2]string{query.Get("since"), query.Get("until")})
		}
	}
	if len(windows) != 2 {
		t.Fatalf("collector page-one queries = %d, want one per scheduler run", len(windows))
	}
	if windows[0] != windows[1] {
		t.Fatalf("retry window = %v, first window = %v", windows[1], windows[0])
	}
	if got := idx.Len(); got != len(rows) {
		t.Fatalf("retry retained %d identity candidates, want %d (one per distinct row)", got, len(rows))
	}

	for _, row := range rows {
		host, _ := splitAppDomain(row.AppDomain)
		got := idx.Lookup(row.IPAddress, host, row.CreatedAt)
		want := identity.Match{UserEmail: row.UserEmail, LoginRayID: row.RayID, Inferred: true}
		if got != want {
			t.Errorf("lookup for %s at %s = %+v, want %+v", row.IPAddress, row.CreatedAt, got, want)
		}
	}
	mark, ok := store.Get(entry.Collector.Name())
	if !ok || !mark.Equal(at.Add(time.Minute)) {
		t.Fatalf("checkpoint = %s present=%v, want %s after successful retry", mark, ok, at.Add(time.Minute))
	}
}
