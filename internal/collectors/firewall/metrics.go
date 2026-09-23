package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type metrics struct {
	cfg *config.Config
	api cfapi.Client
}

func NewMetrics(cfg *config.Config, api cfapi.Client) *metrics {
	return &metrics{cfg: cfg, api: api}
}

func (*metrics) Name() string                   { return "firewall.metrics" }
func (*metrics) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*metrics) Lag() time.Duration             { return 2 * time.Minute }

type firewallMetric struct {
	value float64
	attrs []telemetry.Attr
}

func (c *metrics) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, fmt.Errorf("invalid firewall metrics window")
	}
	selected, err := zones(ctx, c.cfg, c.api)
	if err != nil {
		return from, err
	}

	var samples []firewallMetric
	var retentionGaps []error
	for _, zone := range selected {
		dataset, settings, err := c.datasetForZone(ctx, zone)
		if err != nil {
			return from, err
		}
		slog.DebugContext(ctx, "selected firewall Groups dataset", "zone", zone.Name, "dataset", dataset)

		hasAction := hasAvailableField(settings.AvailableFields, "dimensions.action")
		hasSource := hasAvailableField(settings.AvailableFields, "dimensions.source")
		// Live ByTimeGroups settings advertise these fields, but the dataset
		// rejects both dimensions.action and dimensions.source in a query.
		if dataset == byTimeGroupsDataset {
			hasAction, hasSource = false, false
		}
		if !hasAvailableField(settings.AvailableFields, "count") {
			return from, fmt.Errorf("zone firewall Groups dataset %s has no count field", dataset)
		}
		wanted := []string{"count"}
		if hasAction {
			wanted = append(wanted, "dimensions.action")
		}
		if hasSource {
			wanted = append(wanted, "dimensions.source")
		}

		var rows []map[string]any
		err = c.api.Query(ctx, cfapi.GraphQLRequest{
			Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: dataset,
			WantedFields: wanted, From: from, To: to, Limit: queryLimit,
		}, &rows)
		if err != nil {
			var gap *cfapi.RetentionGapError
			if errors.As(err, &gap) {
				retentionGaps = append(retentionGaps, fmt.Errorf("zone firewall Groups dataset %s: %w", dataset, err))
				continue
			}
			return from, fmt.Errorf("zone firewall Groups dataset %s: %w", dataset, err)
		}
		if len(rows) >= queryLimit {
			return from, fmt.Errorf("zone firewall Groups dataset %s reached the query limit", dataset)
		}

		for _, row := range rows {
			count, ok := numericValue(row["count"])
			if !ok {
				return from, fmt.Errorf("zone firewall Groups dataset %s returned a row without count", dataset)
			}
			if count <= 0 {
				continue
			}
			dimensions, _ := row["dimensions"].(map[string]any)
			attrs := []telemetry.Attr{{Key: semconv.AttrFirewallZone, Value: zone.Name}}
			if hasAction {
				if value := dimensions["action"]; value != nil {
					attrs = append(attrs, telemetry.Attr{Key: semconv.AttrFirewallAction, Value: valueString(value)})
				}
			}
			if hasSource {
				if value := dimensions["source"]; value != nil {
					attrs = append(attrs, telemetry.Attr{Key: semconv.AttrFirewallSource, Value: valueString(value)})
				}
			}
			samples = append(samples, firewallMetric{value: count, attrs: attrs})
		}

		var missing []string
		if !hasAction {
			missing = append(missing, "action")
		}
		if !hasSource {
			missing = append(missing, "source")
		}
		if len(missing) > 0 {
			slog.WarnContext(ctx, "firewall metric dimensions are unavailable; emitting available dimensions", "zone", zone.Name, "dataset", dataset, "missing_dimensions", strings.Join(missing, ","))
		}
	}
	if len(retentionGaps) > 0 {
		return from, errors.Join(retentionGaps...)
	}

	for _, sample := range samples {
		if err := out.Counter(ctx, semconv.MetricFirewallEvents, sample.value, sample.attrs...); err != nil {
			return from, err
		}
	}
	return to, nil
}

func (c *metrics) datasetForZone(ctx context.Context, zone cfapi.Zone) (string, cfapi.DatasetSettings, error) {
	preferred, err := datasetSettings(ctx, c.api, zone.ID, groupsDataset)
	if err != nil {
		return "", cfapi.DatasetSettings{}, fmt.Errorf("zone firewall metric settings: %w", err)
	}
	if preferred.Enabled {
		return groupsDataset, preferred, nil
	}

	fallback, err := datasetSettings(ctx, c.api, zone.ID, byTimeGroupsDataset)
	if err != nil {
		return "", cfapi.DatasetSettings{}, fmt.Errorf("zone firewall metric settings: %w", err)
	}
	if !fallback.Enabled {
		return "", cfapi.DatasetSettings{}, fmt.Errorf("zone has neither firewall Groups dataset enabled")
	}
	return byTimeGroupsDataset, fallback, nil
}

func numericValue(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		parsed, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed, err == nil
	}
}

var _ collector.WindowCollector = (*metrics)(nil)
