package tunnels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type tunnelRow struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Status      string          `json:"status"`
	DeletedAt   *string         `json:"deleted_at"`
	Connections []connectionRow `json:"connections"`
}
type connectionRow struct {
	ClientID         string `json:"client_id"`
	ClientVersion    string `json:"client_version"`
	Colo             string `json:"colo_name"`
	PendingReconnect bool   `json:"is_pending_reconnect"`
}

type statusCollector struct {
	api       cfapi.Client
	accountID string
	mu        sync.Mutex
	previous  map[string]string
}

func (*statusCollector) Name() string                   { return "tunnels.status" }
func (*statusCollector) DefaultInterval() time.Duration { return time.Minute }

func (c *statusCollector) rows(ctx context.Context) ([]tunnelRow, error) {
	if c.api == nil || c.accountID == "" {
		return nil, fmt.Errorf("tunnels requires API and account ID")
	}
	pager, ok := c.api.(cfapi.PageGetter)
	if !ok {
		return nil, fmt.Errorf("tunnels requires API pagination metadata")
	}
	account := url.PathEscape(c.accountID)
	path := "/accounts/" + account + "/cfd_tunnel"
	var all []tunnelRow
	for page := 1; page <= 10000; page++ {
		q := url.Values{"is_deleted": {"false"}, "page": {strconv.Itoa(page)}, "per_page": {"100"}}
		var envelope cfapi.Page
		if err := pager.GetPage(ctx, path, q, &envelope); err != nil {
			return nil, fmt.Errorf("tunnel list: %w", err)
		}
		var rows []tunnelRow
		if err := json.Unmarshal(envelope.Result, &rows); err != nil {
			return nil, fmt.Errorf("tunnel rows: %w", err)
		}
		all = append(all, rows...)
		size := envelope.ResultInfo.PerPage
		if size <= 0 {
			size = 100
		}
		// Ignore total_count: a server can report zero even when results are present.
		if len(rows) < size {
			return all, nil
		}
	}
	return nil, fmt.Errorf("tunnel pagination exceeded 10000 pages")
}

// Collect reads one complete snapshot before emitting anything. Previous status is
// process-local: the first observation of a tunnel (including after restart) emits
// no event. Failed reads or emissions never advance the previous-status snapshot.
func (c *statusCollector) Collect(ctx context.Context, e telemetry.Emitter) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.rows(ctx)
	if err != nil {
		return err
	}
	observed := time.Now()
	next := make(map[string]string, len(rows))
	b := &telemetry.Buffer{}
	for _, row := range rows {
		if row.DeletedAt != nil {
			continue
		}
		if row.ID == "" || row.Status == "" {
			return fmt.Errorf("tunnel row missing id or status")
		}
		if _, exists := next[row.ID]; exists {
			return fmt.Errorf("duplicate tunnel in snapshot")
		}
		next[row.ID] = row.Status
		base := []telemetry.Attr{{Key: semconv.AttrTunnelID, Value: row.ID}, {Key: semconv.AttrTunnelName, Value: row.Name}}
		current := append(append([]telemetry.Attr(nil), base...), telemetry.Attr{Key: semconv.AttrTunnelStatus, Value: row.Status})
		if err := b.Gauge(ctx, semconv.MetricTunnelStatus, 1, current...); err != nil {
			return err
		}
		active := map[string]int{}
		connectors := map[string]map[string]bool{}
		for _, conn := range row.Connections {
			if conn.Colo != "" {
				// Keep the observed colo with zero when every connection is reconnecting.
				if _, ok := active[conn.Colo]; !ok {
					active[conn.Colo] = 0
				}
				if !conn.PendingReconnect {
					active[conn.Colo]++
				}
			}
			if conn.ClientID != "" && conn.ClientVersion != "" {
				if connectors[conn.ClientVersion] == nil {
					connectors[conn.ClientVersion] = map[string]bool{}
				}
				connectors[conn.ClientVersion][conn.ClientID] = true
			}
		}
		for _, colo := range sortedKeys(active) {
			attrs := append(append([]telemetry.Attr(nil), base...), telemetry.Attr{Key: semconv.AttrTunnelColo, Value: colo})
			if err := b.Gauge(ctx, semconv.MetricTunnelConnections, float64(active[colo]), attrs...); err != nil {
				return err
			}
		}
		for _, version := range sortedKeys(connectors) {
			attrs := append(append([]telemetry.Attr(nil), base...), telemetry.Attr{Key: semconv.AttrTunnelConnectorVersion, Value: version})
			if err := b.Gauge(ctx, semconv.MetricTunnelConnectors, float64(len(connectors[version])), attrs...); err != nil {
				return err
			}
		}
		if previous, ok := c.previous[row.ID]; ok && previous != row.Status {
			attrs := append(current, telemetry.Attr{Key: semconv.AttrTunnelPreviousStatus, Value: previous})
			if err := b.LogEvent(ctx, semconv.EventTunnelStatusChange, "Tunnel status changed", observed, otellog.SeverityInfo, attrs...); err != nil {
				return err
			}
		}
	}
	for _, m := range b.Metrics {
		if err := m.Replay(ctx, e); err != nil {
			return err
		}
	}
	for _, r := range b.Records {
		if err := r.Replay(ctx, e); err != nil {
			return err
		}
	}
	c.previous = next
	return nil
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
