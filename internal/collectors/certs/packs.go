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
	api    cfapi.Client
	mu     sync.Mutex
	warned map[string]time.Time
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
	successes := 0
	var failures []error
	for _, zone := range zones {
		rows, err := c.readZone(ctx, zone.ID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// HTTP failures are counted by cfapi's shared request observer (including
			// skipped 403s), rather than duplicating the request counter here.
			if permissionDenied(err) {
				c.warnPermission(ctx, zone.Name, now)
			}
			failures = append(failures, fmt.Errorf("certificate packs: %w", err))
			continue
		}
		successes++
		for _, p := range rows {
			expiry, known := earliestExpiry(p)
			if !known {
				continue
			} // Never turn absent/malformed expiry into a zero gauge.
			attrs := []telemetry.Attr{
				{Key: semconv.AttrCertificateZone, Value: zone.Name},
				{Key: semconv.AttrCertificatePackID, Value: p.ID},
				{Key: semconv.AttrCertificateType, Value: p.Type},
				{Key: semconv.AttrCertificateAuthority, Value: p.Authority},
				{Key: semconv.AttrCertificateStatus, Value: p.Status},
			}
			if err := e.Gauge(ctx, semconv.MetricCertificateExpiry, expiry.Sub(now).Seconds(), attrs...); err != nil {
				return err
			}
		}
	}
	if successes == 0 {
		if len(failures) > 0 {
			return errors.Join(failures...)
		}
		return errors.New("certificate packs: no discovered zones read successfully")
	}
	return nil
}
