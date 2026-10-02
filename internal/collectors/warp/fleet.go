// Package warp exports doc-derived, recently seen WARP fleet counts.
package warp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// Register installs an opt-in snapshot, using the scheduler's passed emitter.
func Register(deps collector.Deps) {
	cfg := deps.Config.Collector(semconv.CollectorNameWARPFleet)
	if !cfg.Enabled {
		return
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	deps.Registry.RegisterSnapshot(&fleetCollector{api: deps.API, account: deps.Config.Cloudflare.AccountID, window: deps.Config.WARP.LastSeenWindow, limit: deps.Config.WARP.MaxMetricSeries, interval: interval}, interval)
}

type fleetCollector struct {
	api      cfapi.Client
	account  string
	window   time.Duration
	limit    int
	interval time.Duration
}

func (*fleetCollector) Name() string                   { return semconv.CollectorNameWARPFleet }
func (*fleetCollector) DefaultInterval() time.Duration { return 5 * time.Minute }

type dimensions [5]string

// Only the seven required strings are decoded; extra user/device data is ignored.
type deviceRow struct {
	ID        *string `json:"deviceId"`
	Timestamp *string `json:"timestamp"`
	Status    *string `json:"status"`
	Platform  *string `json:"platform"`
	Version   *string `json:"version"`
	Mode      *string `json:"mode"`
	Colo      *string `json:"colo"`
}
type devicePage struct {
	Result json.RawMessage `json:"result"`
	Info   *struct {
		Page    int `json:"page"`
		PerPage int `json:"per_page"`
	} `json:"result_info"`
}

func (c *fleetCollector) counts(ctx context.Context, now time.Time) (map[dimensions]int, error) {
	pager, ok := c.api.(cfapi.PageGetter)
	if !ok || c.account == "" {
		return nil, fmt.Errorf("WARP requires pagination API and account")
	}
	if c.window <= 0 || c.window > time.Hour || c.limit < 1 || c.limit > 5000 || c.interval <= 0 {
		return nil, fmt.Errorf("invalid WARP snapshot configuration")
	}
	seen := map[string]dimensions{}
	counts := map[dimensions]int{}
	path := "/accounts/" + url.PathEscape(c.account) + "/dex/fleet-status/devices"
	for page := 1; page <= 10000; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q := url.Values{"source": {"last_seen"}, "page": {strconv.Itoa(page)}, "per_page": {"50"}, "from": {now.Add(-c.window).UTC().Format("2006-01-02T15:04:05.000Z")}, "to": {now.UTC().Format("2006-01-02T15:04:05.000Z")}}
		var envelope devicePage
		if err := pager.GetPage(ctx, path, q, &envelope); err != nil {
			// Do not include upstream bodies or device values in collector errors.
			return nil, fmt.Errorf("WARP page %d fetch failed", page)
		}
		size := 50
		if envelope.Info != nil {
			if envelope.Info.Page != 0 && envelope.Info.Page != page {
				return nil, fmt.Errorf("WARP page %d metadata mismatch", page)
			}
			if envelope.Info.PerPage < 0 || envelope.Info.PerPage > 50 {
				return nil, fmt.Errorf("WARP page %d invalid page size", page)
			}
			if envelope.Info.PerPage > 0 {
				size = envelope.Info.PerPage
			}
		}
		var rows []deviceRow
		if err := json.Unmarshal(envelope.Result, &rows); err != nil || rows == nil {
			return nil, fmt.Errorf("WARP page %d invalid result array", page)
		}
		if len(rows) > size {
			return nil, fmt.Errorf("WARP page %d oversized result", page)
		}
		added := 0
		for _, row := range rows {
			if row.ID == nil || *row.ID == "" || row.Timestamp == nil || row.Status == nil || row.Platform == nil || row.Version == nil || row.Mode == nil || row.Colo == nil {
				return nil, fmt.Errorf("WARP page %d missing required string", page)
			}
			d := dimensions{*row.Status, *row.Platform, *row.Version, *row.Mode, *row.Colo}
			if prior, exists := seen[*row.ID]; exists {
				if prior != d {
					return nil, fmt.Errorf("WARP page %d conflicting duplicate device", page)
				}
				continue
			}
			if len(seen) >= 100000 {
				return nil, fmt.Errorf("WARP unique device limit exceeded")
			}
			seen[*row.ID] = d
			counts[d]++
			added++
		}
		if len(rows) < size {
			return counts, nil
		}
		if added == 0 {
			return nil, fmt.Errorf("WARP page %d pagination made no progress", page)
		}
	}
	return nil, fmt.Errorf("WARP pagination exceeded 10000 pages")
}

func (c *fleetCollector) Collect(ctx context.Context, e telemetry.Emitter) error {
	batch, ok := e.(telemetry.SnapshotBatchEmitter)
	if !ok {
		return fmt.Errorf("WARP requires expiring snapshot batch emitter")
	}
	counts, err := c.counts(ctx, time.Now())
	if err != nil {
		return err
	}
	keys := make([]dimensions, 0, len(counts))
	remainder := 0
	for d, count := range counts {
		long := false
		for _, value := range d {
			if len(value) > 256 {
				long = true
			}
		}
		if long {
			remainder += count
		} else {
			keys = append(keys, d)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		for k := range keys[i] {
			if keys[i][k] != keys[j][k] {
				return keys[i][k] < keys[j][k]
			}
		}
		return false
	})
	points := make([]telemetry.GaugePoint, 0, min(len(keys), c.limit)+1)
	for i, d := range keys {
		if i >= c.limit {
			remainder += counts[d]
			continue
		}
		points = append(points, point(d, counts[d], "false"))
	}
	if remainder > 0 {
		points = append(points, point(dimensions{"other", "other", "other", "other", "other"}, remainder, "true"))
	}
	return batch.GaugeSnapshots(ctx, 3*c.interval, map[string][]telemetry.GaugePoint{semconv.MetricWARPDevices: points})
}

func point(d dimensions, count int, remainder string) telemetry.GaugePoint {
	return telemetry.GaugePoint{Value: float64(count), Attrs: []telemetry.Attr{
		{Key: semconv.AttrWARPStatus, Value: d[0]}, {Key: semconv.AttrWARPPlatform, Value: d[1]}, {Key: semconv.AttrWARPClientVersion, Value: d[2]}, {Key: semconv.AttrWARPMode, Value: d[3]}, {Key: semconv.AttrWARPColo, Value: d[4]}, {Key: semconv.AttrWARPRemainder, Value: remainder},
	}}
}
