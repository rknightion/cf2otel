package certs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// Five is the endpoint's minimum page size. Use short-page termination because
// cfapi.Get intentionally returns only result, not result_info.
const pageSize = 5

type pack struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Authority    string `json:"certificate_authority"`
	Status       string `json:"status"`
	Certificates []struct {
		ExpiresOn string `json:"expires_on"`
	} `json:"certificates"`
}

type packs struct {
	api      cfapi.Client
	interval time.Duration
	mu       sync.Mutex
	warned   map[string]time.Time
}

func (*packs) Name() string                   { return "certs.packs" }
func (*packs) DefaultInterval() time.Duration { return time.Hour }

func (c *packs) readZone(ctx context.Context, zone string) ([]pack, error) {
	path := "/zones/" + url.PathEscape(zone) + "/ssl/certificate_packs"
	var all []pack
	for page := 1; page <= 10000; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q := url.Values{"status": {"all"}, "page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(pageSize)}}
		var rows []pack
		if err := c.api.Get(ctx, path, q, &rows); err != nil {
			return nil, err
		}
		if rows == nil {
			return nil, errors.New("certificate packs: null result")
		}
		all = append(all, rows...)
		if len(rows) < pageSize {
			return all, nil
		}
	}
	return nil, errors.New("certificate pagination exceeded 10000 pages")
}

func permissionDenied(err error) bool {
	var httpErr *cfapi.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == 403 || httpErr.Code == 9109
	}
	// cfapi reports unsuccessful 2xx envelopes as a sanitized numeric-code error.
	return strings.Contains(err.Error(), "cloudflare API: code 9109")
}

func (c *packs) warnPermission(ctx context.Context, zone string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, ok := c.warned[zone]; ok && now.Sub(last) < time.Hour {
		return
	}
	if c.warned == nil {
		c.warned = make(map[string]time.Time)
	}
	c.warned[zone] = now
	slog.WarnContext(ctx, "certificate packs permission denied; skipping zone", "collector", c.Name(), "zone", zone)
}

func earliestExpiry(p pack) (time.Time, bool) {
	var earliest time.Time
	for _, cert := range p.Certificates {
		expiry, err := time.Parse(time.RFC3339, cert.ExpiresOn)
		if err != nil {
			continue
		}
		if earliest.IsZero() || expiry.Before(earliest) {
			earliest = expiry
		}
	}
	return earliest, !earliest.IsZero()
}

func (c *packs) Collect(ctx context.Context, e telemetry.Emitter) error {
	if c.api == nil {
		return errors.New("certificate packs requires API")
	}
	zones, err := c.api.Zones(ctx)
	if err != nil {
		return fmt.Errorf("certificate zones: %w", err)
	}
	now := time.Now()
	var expiryPoints, packPoints []telemetry.GaugePoint
	var failures []error
	for _, zone := range zones {
		rows, err := c.readZone(ctx, zone.ID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// cfapi's HTTP observer counts the actual transport status. An
			// unsuccessful 2xx envelope needs a separate logical-error counter;
			// HTTP errors (including 403) are already visible to that observer.
			var httpErr *cfapi.HTTPError
			if !errors.As(err, &httpErr) && err.Error() == "cloudflare API: code 9109" {
				if emitErr := e.Counter(ctx, semconv.MetricAPIEnvelopeErrors, 1,
					telemetry.Attr{Key: semconv.AttrStatusClass, Value: "4xx"}); emitErr != nil {
					return fmt.Errorf("certificate API envelope counter: %w", emitErr)
				}
			}
			if permissionDenied(err) {
				c.warnPermission(ctx, zone.Name, now)
			}
			failures = append(failures, fmt.Errorf("certificate packs: %w", err))
			continue
		}
		for _, p := range rows {
			attrs := []telemetry.Attr{
				{Key: semconv.AttrCertificateZone, Value: zone.Name},
				{Key: semconv.AttrCertificatePackID, Value: p.ID},
				{Key: semconv.AttrCertificateType, Value: p.Type},
				{Key: semconv.AttrCertificateAuthority, Value: p.Authority},
				{Key: semconv.AttrCertificateStatus, Value: p.Status},
			}
			packPoints = append(packPoints, telemetry.GaugePoint{Value: 1, Attrs: attrs})
			if expiry, known := earliestExpiry(p); known {
				expiryPoints = append(expiryPoints, telemetry.GaugePoint{Value: expiry.Sub(now).Seconds(), Attrs: attrs})
			}
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	interval := c.interval
	if interval <= 0 {
		interval = c.DefaultInterval()
	}
	if emitter, ok := e.(telemetry.SnapshotBatchEmitter); ok {
		return emitter.GaugeSnapshots(ctx, 3*interval, map[string][]telemetry.GaugePoint{
			semconv.MetricCertificateExpiry: expiryPoints,
			semconv.MetricCertificatePack:   packPoints,
		})
	}
	if _, ok := e.(telemetry.SnapshotEmitter); ok {
		return errors.New("certificate snapshots require atomic batch publication")
	}
	for _, snapshot := range []struct {
		name   string
		points []telemetry.GaugePoint
	}{{semconv.MetricCertificateExpiry, expiryPoints}, {semconv.MetricCertificatePack, packPoints}} {
		for _, point := range snapshot.points {
			if err := e.Gauge(ctx, snapshot.name, point.Value, point.Attrs...); err != nil {
				return err
			}
		}
	}
	return nil
}
