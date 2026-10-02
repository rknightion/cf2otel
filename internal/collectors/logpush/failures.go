package logpush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const failuresCollectorName = "logpush.failures"

var failureFields = []string{"sum.uploads", "dimensions.datetimeFiveMinutes", "dimensions.jobId", "dimensions.destinationType", "dimensions.status", "dimensions.final", "dimensions.success"}

type failureMetrics struct {
	cfg *config.Config
	api cfapi.Client
}

func (*failureMetrics) Name() string                   { return failuresCollectorName }
func (*failureMetrics) DefaultInterval() time.Duration { return logpushBucket }
func (*failureMetrics) Lag() time.Duration             { return 10 * time.Minute }

// LagAt exposes only complete UTC five-minute buckets to the scheduler.
func (c *failureMetrics) LagAt(now time.Time) time.Duration {
	return now.Sub(now.UTC().Add(-c.Lag()).Truncate(5 * time.Minute))
}

type failureScope struct {
	scope    cfapi.Scope
	id, zone string
}
type failureRow struct {
	Dimensions struct {
		Bucket      string          `json:"datetimeFiveMinutes"`
		Job         json.RawMessage `json:"jobId"`
		Destination *string         `json:"destinationType"`
		Status      json.RawMessage `json:"status"`
		Final       json.RawMessage `json:"final"`
		Success     json.RawMessage `json:"success"`
	} `json:"dimensions"`
	Sum struct {
		Uploads json.RawMessage `json:"uploads"`
	} `json:"sum"`
}
type failureKey struct{ Scope, Zone, Job, Destination, Status, Final string }

// CollectWindow counts source Groups upload sums, not raw rows or sampled events.
// Only success=0 denotes a failure. A final=true/status>=300 series identifies
// terminal loss per the source schema; retries and final failures remain distinct.
func (c *failureMetrics) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (mark time.Time, collectErr error) {
	ctx, poll := collector.StartZonePoll(ctx)
	defer poll.Finish(ctx, out, c.Name(), &collectErr)
	if c.cfg == nil || c.api == nil || c.cfg.Cloudflare.AccountID == "" || out == nil {
		return from, errors.New("logpush failures requires account, API and emitter")
	}
	cap := c.cfg.Platform.MaxMetricSeriesPerWindow
	if cap <= 0 {
		return from, errors.New("invalid logpush failures platform series cap")
	}
	start, end := from.UTC().Truncate(logpushBucket), to.UTC().Truncate(logpushBucket)
	if start.Before(from) {
		start = start.Add(logpushBucket)
	}
	if !start.Before(end) {
		return from, errors.New("logpush failures window contains no complete bucket")
	}
	reader, ok := c.api.(logpushSettingsReader)
	if !ok {
		return from, errors.New("logpush failures requires dataset settings")
	}
	batch, ok := c.api.(cfapi.GraphQLBatchQuerier)
	if !ok {
		return from, errors.New("logpush failures requires raw batch queries")
	}
	zones, err := c.api.Zones(ctx)
	if err != nil {
		return from, fmt.Errorf("discover Logpush zones: %w", err)
	}
	eligible := make([]cfapi.Zone, 0, len(zones))
	for _, zone := range zones {
		if zone.Account.ID != c.cfg.Cloudflare.AccountID {
			continue
		}
		if zone.ID == "" || strings.TrimSpace(zone.Name) == "" {
			return from, errors.New("incomplete owned Logpush zone")
		}
		eligible = append(eligible, zone)
	}
	zones = collector.SelectPollZones(ctx, zones, eligible, c.cfg.Zones.Exclude, c.cfg.Cloudflare.AccountID)
	scopes := []failureScope{{scope: cfapi.AccountScope, id: c.cfg.Cloudflare.AccountID}}
	seen := map[string]bool{}
	for _, zone := range zones {
		if seen[zone.ID] {
			continue
		}
		seen[zone.ID] = true
		scopes = append(scopes, failureScope{scope: cfapi.ZoneScope, id: zone.ID, zone: zone.Name})
	}
	sort.Slice(scopes[1:], func(i, j int) bool { return scopes[i+1].id < scopes[j+1].id })
	values := map[failureKey]float64{}
	// No emitter calls until every scope and every row has passed validation.
	for _, scope := range scopes {
		settings, err := reader.DatasetSettings(ctx, scope.scope, scope.id, logpushDataset)
		if err != nil {
			return from, fmt.Errorf("read Logpush failure settings: %w", err)
		}
		if !settings.Enabled {
			return from, errors.New("logpush failure dataset disabled")
		}
		if settings.MaxPageSize <= 0 || settings.MaxDuration < int64(logpushBucket/time.Second) || settings.NotOlderThan <= 0 {
			return from, errors.New("invalid Logpush failure page, duration or retention limit")
		}
		if settings.MaxNumberOfFields < len(failureFields) {
			return from, errors.New("logpush failure field budget below required selection")
		}
		for _, field := range failureFields {
			if !logpushHasAvailableField(settings.AvailableFields, field) {
				return from, fmt.Errorf("logpush failure required field unavailable: %s", field)
			}
		}
		if start.Before(time.Now().Add(-time.Duration(settings.NotOlderThan) * time.Second)) {
			return from, &cfapi.RetentionGapError{Dataset: logpushDataset, Floor: time.Now().Add(-time.Duration(settings.NotOlderThan) * time.Second)}
		}
		limit := min(logpushQueryLimit, settings.MaxPageSize)
		maxDuration := (time.Duration(settings.MaxDuration) * time.Second).Truncate(logpushBucket)
		if scope.scope == cfapi.ZoneScope {
			poll.Process(scope.id)
		}
		for leafStart := start; leafStart.Before(end); {
			leafEnd := leafStart.Add(maxDuration)
			if leafEnd.After(end) {
				leafEnd = end
			}
			rows, err := collector.Bisect(leafStart, leafEnd, logpushBucket, logpushBucket, func(a, b time.Time) ([]failureRow, bool, error) {
				request := cfapi.GraphQLRequest{Scope: scope.scope, ScopeID: scope.id, Dataset: logpushDataset, WantedFields: append([]string(nil), failureFields...), From: a, To: b, Limit: limit}
				const alias = "failures"
				result, queryErr := batch.QueryBatch(ctx, []cfapi.GraphQLBatchSelection{{Alias: alias, Request: request}})
				if queryErr != nil {
					return nil, logpushIsSaturation(queryErr), queryErr
				}
				// Ordinary Query normalizes a null dataset into an empty array.
				// Keep the raw boundary strict so a failed scope cannot advance a window.
				raw := strings.TrimSpace(string(result[alias]))
				if !strings.HasPrefix(raw, "[") {
					return nil, false, errors.New("logpush failure response missing row array")
				}
				var rows []failureRow
				if err := json.Unmarshal([]byte(raw), &rows); err != nil {
					return nil, false, err
				}
				if len(rows) >= limit {
					return nil, true, nil
				}
				for _, row := range rows {
					bucket, e := time.Parse(time.RFC3339Nano, row.Dimensions.Bucket)
					if e != nil || !bucket.Equal(bucket.Truncate(logpushBucket)) || bucket.Before(a) || bucket.Add(logpushBucket).After(b) {
						return nil, false, errors.New("logpush failure returned invalid or out-of-window bucket")
					}
				}
				return rows, false, nil
			})
			if err != nil {
				return from, fmt.Errorf("query Logpush failures: %w", err)
			}
			for _, row := range rows {
				success, err := failureUint(row.Dimensions.Success, 8)
				if err != nil || success > 1 {
					return from, errors.New("invalid or missing Logpush success flag")
				}
				final, err := failureUint(row.Dimensions.Final, 8)
				if err != nil || final > 1 {
					return from, errors.New("invalid or missing Logpush final flag")
				}
				status, err := failureUint(row.Dimensions.Status, 16)
				if err != nil {
					return from, errors.New("invalid or missing Logpush status")
				}
				job, err := failureUint(row.Dimensions.Job, 64)
				if err != nil {
					return from, errors.New("invalid or missing Logpush job ID")
				}
				uploads, err := failureUint(row.Sum.Uploads, 64)
				if err != nil {
					return from, errors.New("invalid or missing Logpush upload sum")
				}
				if row.Dimensions.Destination == nil || !utf8.ValidString(*row.Dimensions.Destination) {
					return from, errors.New("invalid or missing Logpush destination type")
				}
				destination := []rune(*row.Dimensions.Destination)
				if len(destination) > 128 {
					destination = destination[:128]
				}
				if success == 1 {
					continue
				}
				key := failureKey{Scope: string(scope.scope), Zone: scope.zone, Job: strconv.FormatUint(job, 10), Destination: string(destination), Status: strconv.FormatUint(status, 10), Final: strconv.FormatBool(final == 1)}
				value := values[key] + float64(uploads)
				if math.IsInf(value, 0) {
					return from, errors.New("logpush failure upload sum overflow")
				}
				values[key] = value
			}
			leafStart = leafEnd
		}
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}
	keys := make([]failureKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { a, b := keys[i], keys[j]; return failureSortKey(a) < failureSortKey(b) })
	if len(keys) > cap {
		slog.WarnContext(ctx, "platform metric series dropped", "collector", failuresCollectorName, "dropped_series", len(keys)-cap)
		keys = keys[:cap]
	}
	for _, key := range keys {
		attrs := []telemetry.Attr{{Key: semconv.AttrLogpushScope, Value: key.Scope}, {Key: semconv.AttrLogpushJobID, Value: key.Job}, {Key: semconv.AttrLogpushDestinationType, Value: key.Destination}, {Key: semconv.AttrLogpushStatusCode, Value: key.Status}, {Key: semconv.AttrLogpushFinalAttempt, Value: key.Final}}
		if key.Scope == string(cfapi.ZoneScope) {
			attrs = append(attrs, telemetry.Attr{Key: semconv.AttrLogpushZone, Value: key.Zone})
		}
		if err := out.Counter(ctx, semconv.MetricLogpushFailedUploads, values[key], attrs...); err != nil {
			return from, err
		}
	}
	return end, nil
}

func failureSortKey(k failureKey) string { b, _ := json.Marshal(k); return string(b) }

// Parse raw JSON integers to retain all uint64 job ID bits and reject null,
// strings, fractions, booleans and out-of-range source unsigned values.
func failureUint(raw json.RawMessage, bits int) (uint64, error) {
	return strconv.ParseUint(string(raw), 10, bits)
}

var _ collector.WindowCollector = (*failureMetrics)(nil)
