package audit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const pageLimit = 100

type auditEvent struct {
	ID     string `json:"id"`
	Action struct {
		Type, Description, Result string
		Time                      time.Time `json:"time"`
	} `json:"action"`
	Actor struct {
		ID, Email, Type string
		IPAddress       string                    `json:"ip_address"`
		Token           struct{ ID, Name string } `json:"token"`
	} `json:"actor"`
	Resource struct{ Type, ID, Product string } `json:"resource"`
	Raw      struct {
		RayID      string `json:"cf_ray_id"`
		Method     string `json:"method"`
		StatusCode int    `json:"status_code"`
		URI        string `json:"uri"`
		UserAgent  string `json:"user_agent"`
	} `json:"raw"`
}

type auditPage struct {
	Result     []auditEvent `json:"result"`
	ResultInfo struct {
		Cursor string `json:"cursor"`
	} `json:"result_info"`
}

type logs struct{ deps collector.Deps }

func newLogs(deps collector.Deps) *logs      { return &logs{deps: deps} }
func (*logs) Name() string                   { return "audit.logs" }
func (*logs) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*logs) Lag() time.Duration             { return 2 * time.Minute }

func (c *logs) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) || c.deps.Config == nil || c.deps.API == nil || out == nil {
		return time.Time{}, errors.New("audit logs: invalid window or dependencies")
	}
	getter, ok := c.deps.API.(cfapi.PageGetter)
	if !ok {
		return time.Time{}, errors.New("audit logs: API does not preserve result_info cursor")
	}
	path := "/accounts/" + url.PathEscape(c.deps.Config.Cloudflare.AccountID) + "/logs/audit"
	// Overlap the source query so a provider that treats since as exclusive
	// still returns rows exactly at our durable half-open window boundary.
	query := url.Values{"since": {from.Add(-time.Second).UTC().Format(time.RFC3339Nano)}, "before": {to.UTC().Format(time.RFC3339Nano)}, "limit": {"200"}}
	seenIDs := map[string]bool{}
	seenCursors := map[string]bool{}
	var events []auditEvent
	for pageNumber := 1; pageNumber <= pageLimit; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return time.Time{}, err
		}
		var page auditPage
		if err := getter.GetPage(ctx, path, query, &page); err != nil {
			return time.Time{}, fmt.Errorf("audit logs page %d: %w", pageNumber, err)
		}
		for _, event := range page.Result {
			if event.ID == "" || event.Action.Time.IsZero() {
				return time.Time{}, fmt.Errorf("audit logs page %d: missing id or action time", pageNumber)
			}
			if seenIDs[event.ID] || event.Action.Time.Before(from) || !event.Action.Time.Before(to) {
				continue
			}
			seenIDs[event.ID] = true
			events = append(events, event)
		}
		cursor := page.ResultInfo.Cursor
		if cursor == "" {
			break
		}
		if seenCursors[cursor] {
			return time.Time{}, errors.New("audit logs: repeated cursor")
		}
		if pageNumber == pageLimit {
			return time.Time{}, errors.New("audit logs: page limit reached")
		}
		seenCursors[cursor] = true
		query.Set("cursor", cursor)
	}
	for _, event := range events {
		if err := emit(ctx, out, event); err != nil {
			return time.Time{}, err
		}
	}
	return to, nil
}

func emit(ctx context.Context, out telemetry.Emitter, e auditEvent) error {
	attrs := []telemetry.Attr{
		{Key: semconv.AttrAuditID, Value: e.ID},
		{Key: semconv.AttrAuditActorID, Value: e.Actor.ID},
		{Key: semconv.AttrAuditActorEmail, Value: e.Actor.Email},
		{Key: semconv.AttrAuditActorIP, Value: e.Actor.IPAddress},
		{Key: semconv.AttrAuditActorTokenID, Value: e.Actor.Token.ID},
		{Key: semconv.AttrAuditActorTokenName, Value: e.Actor.Token.Name},
		{Key: semconv.AttrAuditActorType, Value: e.Actor.Type},
		{Key: semconv.AttrAuditActionType, Value: e.Action.Type},
		{Key: semconv.AttrAuditActionTime, Value: e.Action.Time.UTC().Format(time.RFC3339Nano)},
		{Key: semconv.AttrAuditActionDescription, Value: e.Action.Description},
		{Key: semconv.AttrAuditActionResult, Value: e.Action.Result},
		{Key: semconv.AttrAuditResourceType, Value: e.Resource.Type},
		{Key: semconv.AttrAuditResourceID, Value: e.Resource.ID},
		{Key: semconv.AttrAuditResourceProduct, Value: e.Resource.Product},
		{Key: semconv.AttrAuditRayID, Value: e.Raw.RayID},
		{Key: semconv.AttrAuditMethod, Value: e.Raw.Method},
		{Key: semconv.AttrAuditStatusCode, Value: strconv.Itoa(e.Raw.StatusCode)},
		{Key: semconv.AttrAuditURI, Value: e.Raw.URI},
		{Key: semconv.AttrAuditUserAgent, Value: e.Raw.UserAgent},
	}
	severity := otellog.SeverityInfo
	if !strings.EqualFold(e.Action.Result, "success") {
		severity = otellog.SeverityWarn
	}
	if err := out.LogEvent(ctx, semconv.EventAuditEvent, "Cloudflare audit event", e.Action.Time, severity, attrs...); err != nil {
		return err
	}
	if err := out.Counter(ctx, semconv.MetricAuditEvents, 1,
		telemetry.Attr{Key: semconv.AttrAuditResourceProduct, Value: e.Resource.Product},
		telemetry.Attr{Key: semconv.AttrAuditActionType, Value: e.Action.Type},
		telemetry.Attr{Key: semconv.AttrAuditActionResult, Value: e.Action.Result}); err != nil {
		return err
	}
	return nil
}
