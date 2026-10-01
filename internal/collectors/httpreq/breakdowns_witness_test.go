package httpreq

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
)

type noBatchAPI struct {
	cfapi.Client
	settings httpGroupSettingsProvider
}

func (f noBatchAPI) DatasetSettings(ctx context.Context, scope cfapi.Scope, id, dataset string) (cfapi.DatasetSettings, error) {
	return f.settings.DatasetSettings(ctx, scope, id, dataset)
}

func TestRegisteredBreakdownsRequireBatchClient(t *testing.T) {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "account-fixture"
	cfg.HTTP.MetricsScope = "all"
	cfg.HTTP.Breakdowns = []string{"country"}
	api := &analyticsAPI{budget: 40}
	reg := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: noBatchAPI{Client: api, settings: api}, Registry: reg})
	for _, entry := range reg.Entries() {
		if entry.Collector.Name() != "httpreq.metrics" {
			continue
		}
		from := time.Now().Add(-time.Hour)
		e := &fakeEmitter{}
		mark, err := entry.Collector.(collector.WindowCollector).CollectWindow(context.Background(), from, from.Add(time.Minute), e)
		if err == nil || !strings.Contains(err.Error(), "batch") || !mark.Equal(from) || len(e.counts) != 0 {
			t.Fatalf("enabled breakdowns require batch support without partial emission: mark=%s err=%v counters=%d", mark, err, len(e.counts))
		}
		return
	}
	t.Fatal("metrics collector not registered")
}
