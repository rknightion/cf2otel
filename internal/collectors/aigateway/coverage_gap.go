package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const (
	coverageDataset = "aiGatewayRequestsAdaptiveGroups"
	// coverageWindow is the closed window compared per commit. The scheduler
	// entry uses it as both cadence and MaxWindow, so a steady-state tick
	// commits one window and each gauge value is exported.
	coverageWindow = 5 * time.Minute
	// Groups reached the REST count 139-365 s after the event in loop 3; hold
	// every compared window back well past that.
	coverageLag        = 10 * time.Minute
	coverageQueryLimit = 10000
)

// The coverage selection. Groups count is already sample-corrected.
var coverageFields = []string{"count", "dimensions.gateway"}

type coverageSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

// coverage compares the aiGatewayRequestsAdaptiveGroups request count with the
// REST log row count for each configured gateway. It is a completeness check
// on the REST log source, never a second request rate.
type coverage struct {
	cfg *config.Config
	api cfapi.Client
	now func() time.Time
}

func NewCoverage(cfg *config.Config, api cfapi.Client) *coverage {
	return &coverage{cfg: cfg, api: api, now: time.Now}
}
func (*coverage) Name() string                   { return "aigateway.coverage" }
func (*coverage) DefaultInterval() time.Duration { return 5 * time.Minute }

// Lag holds back at least coverageLag and ends on a window boundary, so a
// scheduler tick sees either a closed window or an empty range it skips.
func (c *coverage) Lag() time.Duration {
	held := c.now().UTC().Add(-coverageLag)
	return coverageLag + held.Sub(held.Truncate(coverageWindow))
}

// CollectWindow commits at most one closed, aligned five-minute window per
// call and returns its end. The gauge value for a window is a pure function of
// that window, so a retried commit recomputes the same window and value; it
// never advances to a newer one or accumulates.
func (c *coverage) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("aigateway coverage: invalid window")
	}
	if c.cfg == nil || c.api == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("aigateway coverage requires a configured Cloudflare account and API")
	}
	start := from.UTC().Truncate(coverageWindow)
	if start.Before(from) {
		start = start.Add(coverageWindow)
	}
	end := start.Add(coverageWindow)
	if end.After(to) {
		// Move an unaligned cursor to the next boundary without comparing the
		// partial window before it; that window is never reported.
		if start.After(from) && !start.After(to) {
			return start, nil
		}
		return from, errors.New("aigateway coverage: window contains no closed five-minute window")
	}
	// The scheduler always registers this collector with a MaxWindow of
	// coverageWindow (Register), so it never asks for more than one closed
	// window itself; only an explicit CollectRange call (the -since/-before
	// path, which passes the operator's raw range straight through in one
	// call) can. Silently exporting the first closed window while reporting
	// success would hide every later window in the range, so refuse before
	// reading or emitting anything.
	if !end.Add(coverageWindow).After(to) {
		return from, fmt.Errorf("aigateway coverage: range %s to %s spans more than one closed %s window; request one window per call", from.Format(time.RFC3339), to.Format(time.RFC3339), coverageWindow)
	}
	// CollectRange calls this without the scheduler lag.
	if end.After(c.now().UTC().Add(-coverageLag)) {
		return from, fmt.Errorf("aigateway coverage: window ending %s is inside the %s holdback", end.Format(time.RFC3339), coverageLag)
	}
	gateways := make([]string, 0, len(c.cfg.AIGateway.Gateways))
	for _, gateway := range c.cfg.AIGateway.Gateways {
		if gateway == "" {
			return from, errors.New("aigateway coverage: empty gateway")
		}
		if !slices.Contains(gateways, gateway) {
			gateways = append(gateways, gateway)
		}
	}
	if len(gateways) == 0 {
		return end, nil
	}
	sort.Strings(gateways)

	groups, err := c.groupsCounts(ctx, start, end)
	if err != nil {
		return from, err
	}
	// Read every source before emitting so a failed read emits nothing.
	gaps := make([]float64, len(gateways))
	for i, gateway := range gateways {
		logged, err := c.restCount(ctx, gateway, start, end)
		if err != nil {
			return from, err
		}
		gaps[i] = groups[gateway] - float64(logged)
	}
	for i, gateway := range gateways {
		if err := out.Gauge(ctx, semconv.MetricAIGatewayLogCoverageGap, gaps[i], telemetry.Attr{Key: semconv.AttrAIGatewayName, Value: gateway}); err != nil {
			return from, err
		}
	}
	return end, nil
}

// groupsCounts reads per-gateway Groups request counts for [start, end).
func (c *coverage) groupsCounts(ctx context.Context, start, end time.Time) (map[string]float64, error) {
	reader, ok := c.api.(coverageSettingsReader)
	if !ok {
		return nil, errors.New("cloudflare client does not expose AI Gateway Groups dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.AccountScope, c.cfg.Cloudflare.AccountID, coverageDataset)
	if err != nil {
		return nil, fmt.Errorf("read AI Gateway Groups settings: %w", err)
	}
	if !settings.Enabled {
		return nil, errors.New("AI Gateway Groups dataset is disabled")
	}
	for _, field := range coverageFields {
		if !coverageFieldAvailable(settings.AvailableFields, field) {
			return nil, fmt.Errorf("AI Gateway Groups dataset is missing required field %s", field)
		}
	}
	if settings.MaxNumberOfFields < len(coverageFields) {
		return nil, errors.New("AI Gateway Groups field limit is below the required field count")
	}
	if settings.MaxPageSize <= 0 {
		return nil, errors.New("AI Gateway Groups dataset is missing its page-size limit")
	}
	if settings.MaxDuration < int64(coverageWindow/time.Second) {
		return nil, errors.New("AI Gateway Groups duration limit cannot contain a five-minute window")
	}
	if settings.NotOlderThan <= 0 {
		return nil, errors.New("AI Gateway Groups dataset is missing its retention limit")
	}
	floor := c.now().UTC().Add(-time.Duration(settings.NotOlderThan) * time.Second)
	if start.Before(floor) {
		return nil, &cfapi.RetentionGapError{Dataset: coverageDataset, Floor: floor}
	}
	limit := min(coverageQueryLimit, settings.MaxPageSize)
	var rows []map[string]any
	err = c.api.Query(ctx, cfapi.GraphQLRequest{
		Scope:        cfapi.AccountScope,
		ScopeID:      c.cfg.Cloudflare.AccountID,
		Dataset:      coverageDataset,
		WantedFields: append([]string(nil), coverageFields...),
		From:         start,
		To:           end,
		Limit:        limit,
	}, &rows)
	if err != nil {
		return nil, fmt.Errorf("query AI Gateway Groups: %w", err)
	}
	// One row per gateway is expected. A full page may be truncated, and a
	// five-minute window cannot be split further.
	if len(rows) >= limit {
		return nil, fmt.Errorf("AI Gateway Groups window reached the requested limit %d", limit)
	}
	counts := make(map[string]float64, len(rows))
	for _, row := range rows {
		dimensions, _ := row["dimensions"].(map[string]any)
		gateway, _ := dimensions["gateway"].(string)
		if gateway == "" {
			return nil, errors.New("AI Gateway Groups row has no gateway")
		}
		count, ok := coverageCount(row["count"])
		if !ok {
			return nil, errors.New("AI Gateway Groups row has an invalid count")
		}
		counts[gateway] += count
	}
	return counts, nil
}

// restCount pages the gateway log list for [start, end) and counts distinct
// log IDs. total_count is not trusted; paging is the authoritative count.
func (c *coverage) restCount(ctx context.Context, gateway string, start, end time.Time) (int, error) {
	type coverageRow struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
	}
	path := "/accounts/" + url.PathEscape(c.cfg.Cloudflare.AccountID) + "/ai-gateway/gateways/" + url.PathEscape(gateway) + "/logs"
	seen := make(map[string]bool)
	for pageNumber := 1; pageNumber <= 10000; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		query := url.Values{"page": {strconv.Itoa(pageNumber)}, "per_page": {strconv.Itoa(logPageSize)}, "order_by": {"created_at"}, "order_by_direction": {"asc"}, "start_date": {start.UTC().Format(time.RFC3339Nano)}, "end_date": {end.UTC().Format(time.RFC3339Nano)}}
		var page []coverageRow
		if err := c.api.Get(ctx, path, query, &page); err != nil {
			return 0, fmt.Errorf("aigateway coverage logs page %d: %w", pageNumber, err)
		}
		pastEnd := false
		for _, row := range page {
			if row.ID == "" || row.CreatedAt.IsZero() {
				return 0, fmt.Errorf("aigateway coverage logs page %d: missing id or created_at", pageNumber)
			}
			// The API bounds are inclusive; the compared window is [start, end).
			if !row.CreatedAt.Before(end) {
				pastEnd = true
				continue
			}
			if row.CreatedAt.Before(start) {
				continue
			}
			seen[row.ID] = true
		}
		if pastEnd || len(page) == 0 {
			return len(seen), nil
		}
	}
	return 0, errors.New("aigateway coverage logs: page limit reached")
}

func coverageFieldAvailable(available []string, wanted string) bool {
	flat := strings.Replace(wanted, ".", "_", 1)
	for _, field := range available {
		if strings.EqualFold(field, wanted) || strings.EqualFold(field, flat) {
			return true
		}
	}
	return false
}

func coverageCount(value any) (float64, bool) {
	var n float64
	switch v := value.(type) {
	case float64:
		n = v
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, false
		}
		n = parsed
	default:
		return 0, false
	}
	return n, n >= 0 && n == math.Trunc(n) && !math.IsInf(n, 0)
}
