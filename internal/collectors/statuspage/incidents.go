package statuspage

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type incidentCollector struct{ source *source }

func (*incidentCollector) Name() string                   { return semconv.CollectorNameStatuspageIncidents }
func (*incidentCollector) DefaultInterval() time.Duration { return 5 * time.Minute }

// This is a best-effort latest-page feed, with no publication-latency holdback.
func (*incidentCollector) Lag() time.Duration { return 0 }

type updateKey struct {
	id string
	at time.Time
}

func (c *incidentCollector) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	b, err := c.source.fetch(ctx, "/api/v2/incidents.json")
	if err != nil {
		return from, err
	}
	rows, err := decodeIncidents(b)
	if err != nil {
		return from, err
	}
	buffered := &telemetry.Buffer{}
	seen := make(map[updateKey]bool)
	for _, r := range rows {
		for _, u := range *r.Updates {
			at, err := time.Parse(time.RFC3339Nano, *u.UpdatedAt)
			if err != nil {
				return from, errors.New("invalid statuspage update timestamp")
			}
			at = at.UTC()
			if at.IsZero() {
				return from, errors.New("invalid statuspage zero update timestamp")
			}
			// Validate every timestamp, even outside this window, before any replay.
			key := updateKey{*u.ID, at}
			if seen[key] {
				continue
			}
			seen[key] = true
			// The durable scheduler checkpoint is exclusive: an update equal to
			// it was covered by the preceding successful commit. Late older
			// publications are intentionally missed; there is no seen-ID ledger.
			if !at.After(from) || at.After(to) {
				continue
			}
			body, truncated := boundedBody(*u.Body)
			attrs := []telemetry.Attr{
				{Key: semconv.AttrStatusIncidentID, Value: *r.ID}, {Key: semconv.AttrStatusIncidentName, Value: *r.Name},
				{Key: semconv.AttrStatusIncidentStatus, Value: *r.Status}, {Key: semconv.AttrStatusIncidentImpact, Value: *r.Impact},
				{Key: semconv.AttrStatusUpdateID, Value: *u.ID}, {Key: semconv.AttrStatusUpdateStatus, Value: *u.Status},
			}
			if truncated {
				attrs = append(attrs, telemetry.Attr{Key: semconv.AttrStatusUpdateTruncated, Value: "true"})
			}
			if err := buffered.LogEvent(ctx, semconv.EventStatusIncidentUpdate, body, at, otellog.SeverityInfo, attrs...); err != nil {
				return from, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}
	if err := buffered.ReplayInto(ctx, e); err != nil {
		return from, err
	}
	return to, nil
}
func boundedBody(body string) (string, bool) {
	if len(body) <= 8192 {
		return body, false
	}
	end := 8192
	for end > 0 && !utf8.RuneStart(body[end]) {
		end--
	}
	return body[:end], true
}
