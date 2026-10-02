package access

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// seatsCollector retains only counts, never a user catalog or user identifiers.
// Cloudflare's AccessUserListResponse has independent boolean seat flags;
// pointers preserve explicit false versus absent/null fields on the wire.
type seatsCollector struct {
	api       cfapi.Client
	accountID string
}

func (seatsCollector) Name() string                   { return semconv.CollectorNameAccessSeats }
func (seatsCollector) DefaultInterval() time.Duration { return 15 * time.Minute }

func (c seatsCollector) Collect(ctx context.Context, e telemetry.Emitter) error {
	if c.api == nil || c.accountID == "" {
		return fmt.Errorf("access seats requires API and account ID")
	}
	const pageSize = 100
	const maxPages = 10000
	var accessCount, gatewayCount int64
	for page := 1; ; page++ {
		var rows []struct {
			AccessSeat  *bool `json:"access_seat"`
			GatewaySeat *bool `json:"gateway_seat"`
		}
		query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(pageSize)}}
		if err := c.api.Get(ctx, "/accounts/"+url.PathEscape(c.accountID)+"/access/users", query, &rows); err != nil {
			return fmt.Errorf("access seats page %d: %w", page, err)
		}
		if rows == nil {
			return fmt.Errorf("access seats page %d: expected a user list, got null", page)
		}
		for _, row := range rows {
			if row.AccessSeat == nil || row.GatewaySeat == nil {
				return fmt.Errorf("access seats page %d: required seat flags absent or null", page)
			}
			if *row.AccessSeat {
				accessCount++
			}
			if *row.GatewaySeat {
				gatewayCount++
			}
		}
		if len(rows) < pageSize {
			break
		}
		if page >= maxPages {
			return fmt.Errorf("access seats pagination exceeded %d pages", maxPages)
		}
	}
	// Publish only after the entire list has been fetched and validated. A user
	// holding both seats contributes once to each series, not to a combined total.
	if err := e.Gauge(ctx, semconv.MetricAccessSeats, float64(accessCount), telemetry.Attr{Key: semconv.AttrAccessSeatType, Value: "access"}); err != nil {
		return err
	}
	return e.Gauge(ctx, semconv.MetricAccessSeats, float64(gatewayCount), telemetry.Attr{Key: semconv.AttrAccessSeatType, Value: "gateway"})
}

func registerSeats(deps collector.Deps) {
	cfg := deps.Config.Collector(semconv.CollectorNameAccessSeats)
	if cfg.Enabled {
		deps.Registry.RegisterSnapshot(seatsCollector{api: deps.API, accountID: deps.Config.Cloudflare.AccountID}, cfg.Interval)
	}
}
