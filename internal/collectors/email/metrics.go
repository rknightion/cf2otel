package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const (
	emailBucket             = 5 * time.Minute
	emailGraphQLMaxLimit    = 10000
	emailDatasetRouting     = "emailRoutingAdaptiveGroups"
	emailDatasetSending     = "emailSendingAdaptiveGroups"
	emailSaturatedErrorText = "saturated limit"
)

type datasetSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

type datasetSpec struct {
	name, dataset, metric string
}

// emailRetentionError stays distinct from cfapi.RetentionGapError so the scheduler fails closed.
type emailRetentionError struct {
	dataset string
	floor   time.Time
}

func (e *emailRetentionError) Error() string {
	return fmt.Sprintf("email dataset %s retention gap: floor %s", e.dataset, e.floor.UTC().Format(time.RFC3339))
}

var (
	routingSpec = datasetSpec{name: "email.routing", dataset: emailDatasetRouting, metric: semconv.MetricEmailRoutingEvents}
	sendingSpec = datasetSpec{name: "email.sending", dataset: emailDatasetSending, metric: semconv.MetricEmailSendingEvents}
)

type metrics struct {
	cfg  *config.Config
	api  cfapi.Client
	spec datasetSpec
}

func newMetrics(cfg *config.Config, api cfapi.Client, spec datasetSpec) *metrics {
	return &metrics{cfg: cfg, api: api, spec: spec}
}

func (c *metrics) Name() string                   { return c.spec.name }
func (c *metrics) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*metrics) Lag() time.Duration               { return 10 * time.Minute }

func (c *metrics) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid email metrics window")
	}
	if c.cfg == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("email metrics require a configured Cloudflare account")
	}
	if c.api == nil {
		return from, errors.New("email metrics require a Cloudflare API client")
	}
	reader, ok := c.api.(datasetSettingsReader)
	if !ok {
		return from, errors.New("cloudflare client does not expose email dataset settings")
	}
	seriesLimit := c.cfg.Platform.MaxMetricSeriesPerWindow
	if seriesLimit < 1 {
		return from, errors.New("email metrics series limit must be positive")
	}

	windowStart := ceilEmailBucket(from)
	windowEnd := floorEmailBucket(to)
	if !windowStart.Before(windowEnd) {
		return from, nil
	}

	zones, err := c.api.Zones(ctx)
	if err != nil {
		return from, fmt.Errorf("list zones for %s: %w", c.spec.name, err)
	}
	selectedZones, err := selectAccountZones(zones, c.cfg.Cloudflare.AccountID, c.cfg.Cloudflare.Zones)
	if err != nil {
		return from, fmt.Errorf("select zones for %s: %w", c.spec.name, err)
	}

	total := float64(0)
	enabledZones := 0
	for _, zone := range selectedZones {
		settings, err := reader.DatasetSettings(ctx, cfapi.ZoneScope, zone.ID, c.spec.dataset)
		if err != nil {
			return from, fmt.Errorf("read %s dataset settings: %w", c.spec.name, err)
		}
		if !settings.Enabled {
			continue
		}
		enabledZones++
		if err := validateEmailSettings(settings, c.spec.dataset, windowStart); err != nil {
			return from, fmt.Errorf("%s zone dataset settings: %w", c.spec.name, err)
		}

		queryLimit := settings.MaxPageSize
		if queryLimit > emailGraphQLMaxLimit {
			queryLimit = emailGraphQLMaxLimit
		}
		maxBuckets := int(settings.MaxDuration / int64(emailBucket/time.Second))
		if maxBuckets > queryLimit {
			maxBuckets = queryLimit
		}
		for segmentStart := windowStart; segmentStart.Before(windowEnd); {
			segmentEnd := segmentStart.Add(time.Duration(maxBuckets) * emailBucket)
			if segmentEnd.After(windowEnd) {
				segmentEnd = windowEnd
			}
			count, err := c.queryCompleteBuckets(ctx, zone.ID, segmentStart, segmentEnd, queryLimit)
			if err != nil {
				return from, fmt.Errorf("query %s for account-owned zone: %w", c.spec.name, err)
			}
			total += count
			if math.IsInf(total, 0) || math.IsNaN(total) {
				return from, errors.New("email metric count overflow")
			}
			segmentStart = segmentEnd
		}
	}
	if enabledZones == 0 {
		return from, errors.New("no account-owned zone has an enabled email Groups dataset")
	}

	// Email datasets expose no allowed metric dimensions, so each collector emits
	// at most one account aggregate and the configured series cap is naturally met.
	if err := out.Counter(ctx, c.spec.metric, total); err != nil {
		return from, err
	}
	return windowEnd, nil
}

func validateEmailSettings(settings cfapi.DatasetSettings, dataset string, from time.Time) error {
	for _, field := range []string{"count", "dimensions.datetimeFiveMinutes"} {
		if !emailHasAvailableField(settings.AvailableFields, field) {
			return fmt.Errorf("missing required field %s", field)
		}
	}
	if settings.MaxNumberOfFields < 2 {
		return errors.New("dataset field limit is too small for count and datetimeFiveMinutes")
	}
	if settings.MaxPageSize <= 0 {
		return errors.New("dataset page-size limit is missing")
	}
	if settings.MaxDuration <= 0 {
		return errors.New("dataset duration limit is missing")
	}
	if settings.NotOlderThan <= 0 {
		return errors.New("dataset retention limit is missing")
	}
	if settings.MaxDuration/int64(emailBucket/time.Second) < 1 {
		return errors.New("dataset duration limit cannot contain a complete five-minute bucket")
	}
	retentionFloor := time.Now().UTC().Add(-time.Duration(settings.NotOlderThan) * time.Second)
	if from.Before(retentionFloor) {
		return &emailRetentionError{dataset: dataset, floor: retentionFloor}
	}
	return nil
}

func emailHasAvailableField(available []string, wanted string) bool {
	for _, field := range available {
		if strings.EqualFold(field, wanted) {
			return true
		}
		if prefix, suffix, ok := strings.Cut(wanted, "."); ok && strings.EqualFold(field, prefix+"_"+suffix) {
			return true
		}
	}
	return false
}

func selectAccountZones(zones []cfapi.Zone, accountID string, configured []string) ([]cfapi.Zone, error) {
	matched := make([]bool, len(configured))
	selected := make([]cfapi.Zone, 0, len(zones))
	seen := make(map[string]bool, len(zones))
	for _, zone := range zones {
		if zone.Account.ID != accountID {
			continue
		}
		included := len(configured) == 0
		for i, nameOrID := range configured {
			if nameOrID != "" && (nameOrID == zone.ID || strings.EqualFold(nameOrID, zone.Name)) {
				matched[i] = true
				included = true
			}
		}
		if !included {
			continue
		}
		if zone.ID == "" {
			return nil, errors.New("account-owned zone has an empty ID")
		}
		if seen[zone.ID] {
			continue
		}
		seen[zone.ID] = true
		selected = append(selected, zone)
	}
	for _, found := range matched {
		if !found {
			return nil, errors.New("configured email zone absent from account-owned discovery")
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("no account-owned email zones were discovered")
	}
	return selected, nil
}

func (c *metrics) queryCompleteBuckets(ctx context.Context, zoneID string, from, to time.Time, limit int) (float64, error) {
	if !from.Before(to) || from != floorEmailBucket(from) || to != floorEmailBucket(to) {
		return 0, errors.New("email GraphQL query is not aligned to complete buckets")
	}
	request := cfapi.GraphQLRequest{
		Scope:        cfapi.ZoneScope,
		ScopeID:      zoneID,
		Dataset:      c.spec.dataset,
		WantedFields: []string{"count", "dimensions.datetimeFiveMinutes"},
		From:         from,
		To:           to,
		Limit:        limit,
	}
	var rows []map[string]any
	if err := c.api.Query(ctx, request, &rows); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), emailSaturatedErrorText) {
			return 0, err
		}
		bucketCount := int(to.Sub(from) / emailBucket)
		if bucketCount <= 1 {
			return 0, fmt.Errorf("irreducible saturated five-minute bucket for dataset %s", c.spec.dataset)
		}
		mid := from.Add(time.Duration(bucketCount/2) * emailBucket)
		left, err := c.queryCompleteBuckets(ctx, zoneID, from, mid, limit)
		if err != nil {
			return 0, err
		}
		right, err := c.queryCompleteBuckets(ctx, zoneID, mid, to, limit)
		if err != nil {
			return 0, err
		}
		return left + right, nil
	}
	return sumEmailRows(rows, from, to)
}

func sumEmailRows(rows []map[string]any, from, to time.Time) (float64, error) {
	var total float64
	seen := make(map[int64]bool, len(rows))
	for _, row := range rows {
		value, ok := row["count"]
		count, valid := emailCount(value)
		if !ok || !valid {
			return 0, errors.New("email Groups row has an invalid count")
		}
		dimensions, ok := row["dimensions"].(map[string]any)
		if !ok {
			return 0, errors.New("email Groups row is missing dimensions")
		}
		timestamp, ok := dimensions["datetimeFiveMinutes"].(string)
		if !ok {
			return 0, errors.New("email Groups row is missing datetimeFiveMinutes")
		}
		bucket, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil || bucket.Second() != 0 || bucket.Nanosecond() != 0 || bucket.Minute()%5 != 0 {
			return 0, errors.New("email Groups row has an invalid five-minute timestamp")
		}
		bucket = bucket.UTC()
		if bucket.Before(from) || !bucket.Before(to) {
			return 0, errors.New("email Groups row timestamp falls outside its query window")
		}
		if seen[bucket.Unix()] {
			return 0, errors.New("email Groups query returned duplicate bucket rows")
		}
		seen[bucket.Unix()] = true
		total += count
		if math.IsInf(total, 0) || math.IsNaN(total) {
			return 0, errors.New("email metric count overflow")
		}
	}
	return total, nil
}

func emailCount(value any) (float64, bool) {
	var count float64
	switch number := value.(type) {
	case float64:
		count = number
	case float32:
		count = float64(number)
	case int:
		count = float64(number)
	case int32:
		count = float64(number)
	case int64:
		count = float64(number)
	case json.Number:
		parsed, err := number.Float64()
		if err != nil {
			return 0, false
		}
		count = parsed
	default:
		return 0, false
	}
	return count, !math.IsNaN(count) && !math.IsInf(count, 0) && count >= 0 && math.Trunc(count) == count
}

func floorEmailBucket(value time.Time) time.Time {
	return value.UTC().Truncate(emailBucket)
}

func ceilEmailBucket(value time.Time) time.Time {
	value = value.UTC()
	floor := floorEmailBucket(value)
	if floor.Before(value) {
		return floor.Add(emailBucket)
	}
	return floor
}
