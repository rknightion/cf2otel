package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// The live AI Gateway endpoint rejects per_page=100 with code 7001.
const logPageSize = 50

// listRowCap bounds how many rows one budgeted call lists before it commits.
// A truncated commit re-lists from its mark, so listing a whole dense window
// first spent most calls on pages a later commit would list again.
const listRowCap = 500

type logs struct {
	cfg *config.Config
	api cfapi.Client
}

func NewLogs(cfg *config.Config, api cfapi.Client) *logs { return &logs{cfg: cfg, api: api} }
func (*logs) Name() string                               { return "aigateway.logs" }
func (*logs) DefaultInterval() time.Duration             { return 5 * time.Minute }
func (*logs) Lag() time.Duration                         { return time.Minute }
func (*logs) AdaptiveCommitWindow() bool                 { return true }

type logRow struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	EventID     string    `json:"event_id"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	ModelType   string    `json:"model_type"`
	Path        string    `json:"path"`
	Duration    *float64  `json:"duration"`
	RequestType string    `json:"request_type"`
	StatusCode  *int      `json:"status_code"`
	Success     *bool     `json:"success"`
	Cached      *bool     `json:"cached"`
	TokensIn    *int64    `json:"tokens_in"`
	TokensOut   *int64    `json:"tokens_out"`
	Usage       *struct {
		Input     *int64 `json:"input_tokens"`
		Output    *int64 `json:"output_tokens"`
		Total     *int64 `json:"total_tokens"`
		Reasoning *int64 `json:"output_reasoning_tokens"`
		Cached    *int64 `json:"input_cached_tokens"`
	} `json:"usage_metadata"`
	Timings *struct {
		Total   *float64 `json:"total"`
		Latency *float64 `json:"latency"`
	} `json:"timings"`
	Location *struct {
		Region string `json:"region"`
		Colo   string `json:"colo"`
	} `json:"location"`
	Cost                 *float64        `json:"cost"`
	CustomCost           json.RawMessage `json:"custom_cost"`
	Metadata             json.RawMessage `json:"metadata"`
	Step                 *int            `json:"step"`
	Feedback             json.RawMessage `json:"feedback"`
	Score                json.RawMessage `json:"score"`
	Prompts              json.RawMessage `json:"prompts"`
	Guardrails           json.RawMessage `json:"guardrails"`
	Authentication       json.RawMessage `json:"authentication"`
	Wholesale            *bool           `json:"wholesale"`
	BYOK                 json.RawMessage `json:"byok"`
	UserAgent            string          `json:"user_agent"`
	DLPAction            string          `json:"dlp_action"`
	DLPProfiles          json.RawMessage `json:"dlp_profiles"`
	RequestHead          json.RawMessage `json:"request_head"`
	ResponseHead         json.RawMessage `json:"response_head"`
	RequestHeadComplete  *bool           `json:"request_head_complete"`
	ResponseHeadComplete *bool           `json:"response_head_complete"`
	RequestSize          *int64          `json:"request_size"`
	ResponseSize         *int64          `json:"response_size"`
	RequestContentType   string          `json:"request_content_type"`
}
type namedRow struct {
	gateway string
	row     logRow
}

// CollectWindow collects complete pages before emitting. A checkpoint advances
// only after the emitter accepts the window. [from,to) excludes inclusive API
// boundaries, and IDs remove duplicates within the same page sequence.
func (c *logs) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	return c.CollectWindowBudget(ctx, from, to, 0, out)
}

// CollectWindowBudget emits whole source seconds in order and returns a
// second boundary before to once the buffered records reach budgetBytes or
// listRowCap rows have been listed. One commit then never delivers, or holds in
// memory, more than the budget plus one second, and bodies past the mark are
// not fetched. The second that would overflow is fetched to be measured and
// left for the next call.
func (c *logs) CollectWindowBudget(ctx context.Context, from, to time.Time, budgetBytes int, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) || c.cfg == nil || c.api == nil {
		return time.Time{}, errors.New("aigateway logs: invalid window or dependencies")
	}
	pagers := make([]*logPager, 0, len(c.cfg.AIGateway.Gateways))
	for _, gateway := range c.cfg.AIGateway.Gateways {
		if gateway == "" {
			return time.Time{}, errors.New("aigateway logs: empty gateway")
		}
		path := "/accounts/" + url.PathEscape(c.cfg.Cloudflare.AccountID) + "/ai-gateway/gateways/" + url.PathEscape(gateway) + "/logs"
		pagers = append(pagers, &logPager{api: c.api, gateway: gateway, path: path, from: from, to: to, seen: map[string]bool{}})
	}
	if budgetBytes <= 0 {
		for _, p := range pagers {
			for !p.done {
				if err := p.next(ctx); err != nil {
					return time.Time{}, err
				}
			}
		}
		for _, item := range takeBefore(pagers, to) {
			if err := c.emit(ctx, item.gateway, item.row, out); err != nil {
				return time.Time{}, err
			}
		}
		return to, nil
	}
	pos, buffered, listed, emitted := from, 0, 0, false
	for {
		horizon := to
		for _, p := range pagers {
			if h := p.horizon(); h.Before(horizon) {
				horizon = h
			}
		}
		// List further while no whole second is known, or below the row cap.
		if !horizon.After(pos) || (horizon.Before(to) && listed < listRowCap) {
			for _, p := range pagers {
				if !p.done && !p.horizon().After(horizon) {
					before := len(p.pending)
					if err := p.next(ctx); err != nil {
						return time.Time{}, err
					}
					listed += len(p.pending) - before
				}
			}
			continue
		}
		rows := takeBefore(pagers, horizon)
		for start := 0; start < len(rows); {
			second := rows[start].row.CreatedAt.Truncate(time.Second)
			end := start
			for end < len(rows) && rows[end].row.CreatedAt.Truncate(time.Second).Equal(second) {
				end++
			}
			group := &telemetry.Buffer{}
			for _, item := range rows[start:end] {
				if err := c.emit(ctx, item.gateway, item.row, group); err != nil {
					return time.Time{}, err
				}
			}
			size := group.ApproxBytes()
			// The first second is always delivered whole, whatever its size.
			if emitted && buffered+size > budgetBytes {
				return second, nil
			}
			if err := group.ReplayInto(ctx, out); err != nil {
				return time.Time{}, err
			}
			buffered += size
			emitted = true
			start = end
		}
		pos = horizon
		if !pos.Before(to) || listed >= listRowCap {
			return pos, nil
		}
	}
}

// logPager lists one gateway's window in ascending created_at order.
type logPager struct {
	api           cfapi.Client
	gateway, path string
	from, to      time.Time
	page          int
	seen          map[string]bool
	pending       []namedRow
	last          time.Time
	done          bool
}

func (p *logPager) next(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.page++
	query := url.Values{"page": {strconv.Itoa(p.page)}, "per_page": {strconv.Itoa(logPageSize)}, "order_by": {"created_at"}, "order_by_direction": {"asc"}, "start_date": {p.from.UTC().Format(time.RFC3339Nano)}, "end_date": {p.to.UTC().Format(time.RFC3339Nano)}}
	var page []logRow
	if err := p.api.Get(ctx, p.path, query, &page); err != nil {
		return fmt.Errorf("aigateway logs page %d: %w", p.page, err)
	}
	horizon := p.horizon()
	pastTo := false
	for _, row := range page {
		if row.ID == "" || row.CreatedAt.IsZero() {
			return fmt.Errorf("aigateway logs page %d: missing id or created_at", p.page)
		}
		if !row.CreatedAt.Before(p.to) {
			pastTo = true
			continue
		}
		if row.CreatedAt.Before(p.from) {
			continue
		}
		key := p.gateway + "\x00" + row.ID
		if p.seen[key] {
			continue
		}
		// An earlier page already let rows before the horizon be emitted.
		if row.CreatedAt.Before(horizon) {
			return fmt.Errorf("aigateway logs page %d: list order went backwards", p.page)
		}
		p.seen[key] = true
		p.pending = append(p.pending, namedRow{p.gateway, row})
	}
	if len(page) > 0 && page[len(page)-1].CreatedAt.After(p.last) {
		p.last = page[len(page)-1].CreatedAt
	}
	// Get unwraps result and does not expose result_info.per_page. The
	// server may cap per_page below our requested size, so only an empty
	// page (or crossing to) proves that pagination is complete.
	if pastTo || len(page) == 0 {
		p.done = true
	} else if p.page == 10000 {
		return errors.New("aigateway logs: page limit reached")
	}
	return nil
}

// horizon is the earliest instant whose rows may still be unlisted. Rows in
// the last listed second can continue on the next page.
func (p *logPager) horizon() time.Time {
	if p.done {
		return p.to
	}
	h := p.last.Truncate(time.Second)
	if h.Before(p.from) {
		return p.from
	}
	if h.After(p.to) {
		return p.to
	}
	return h
}

// takeBefore removes and returns every listed row before limit, in emission order.
func takeBefore(pagers []*logPager, limit time.Time) []namedRow {
	var rows []namedRow
	for _, p := range pagers {
		kept := p.pending[:0]
		for _, item := range p.pending {
			if item.row.CreatedAt.Before(limit) {
				rows = append(rows, item)
			} else {
				kept = append(kept, item)
			}
		}
		p.pending = kept
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.row.CreatedAt.Equal(b.row.CreatedAt) {
			return a.row.ID < b.row.ID
		}
		return a.row.CreatedAt.Before(b.row.CreatedAt)
	})
	return rows
}

func (c *logs) emit(ctx context.Context, gateway string, row logRow, out telemetry.Emitter) error {
	if row.Duration == nil || *row.Duration < 0 || *row.Duration > float64((24*time.Hour)/time.Millisecond) {
		return errors.New("aigateway logs: invalid duration")
	}
	end := row.CreatedAt
	start := end.Add(-time.Duration(*row.Duration * float64(time.Millisecond)))
	operation := operationName(row.Path)
	provider := providerName(row.Provider)
	model := normalizeModel(row.Model)
	attrs := []telemetry.Attr{{Key: semconv.AttrAIGatewayName, Value: gateway}, {Key: semconv.AttrAIGatewayLogID, Value: row.ID}, {Key: semconv.AttrGenAIOperation, Value: operation}}
	add := func(key, value string) {
		if value != "" {
			attrs = append(attrs, telemetry.Attr{Key: key, Value: value})
		}
	}
	add(semconv.AttrAIGatewayEventID, row.EventID)
	add(semconv.AttrAIGatewayProvider, row.Provider)
	add(semconv.AttrGenAIProvider, provider)
	add(semconv.AttrGenAIModel, model)
	add(semconv.AttrAIGatewayModelType, row.ModelType)
	add(semconv.AttrAIGatewayPath, row.Path)
	add(semconv.AttrAIGatewayDurationMS, strconv.FormatFloat(*row.Duration, 'f', -1, 64))
	add(semconv.AttrAIGatewayRequestType, row.RequestType)
	if operation == "other" {
		add(semconv.AttrAIGatewayOperation, "other")
	}
	if row.StatusCode != nil {
		add(semconv.AttrAIGatewayStatusCode, strconv.Itoa(*row.StatusCode))
	}
	if row.Success != nil {
		add(semconv.AttrAIGatewaySuccess, strconv.FormatBool(*row.Success))
	}
	if row.Cached != nil {
		add(semconv.AttrAIGatewayCached, strconv.FormatBool(*row.Cached))
	}
	input, output := row.TokensIn, row.TokensOut
	var total *int64
	bodyUsage := false
	if row.Usage != nil {
		if row.Usage.Input != nil && (*row.Usage.Input != 0 || input == nil || *input <= 0) {
			input = row.Usage.Input
		}
		if row.Usage.Output != nil && (*row.Usage.Output != 0 || output == nil || *output <= 0) {
			output = row.Usage.Output
		}
		total = row.Usage.Total
		addInt(&attrs, semconv.AttrGenAICacheReadTokens, row.Usage.Cached)
		addInt(&attrs, semconv.AttrGenAIReasoningTokens, row.Usage.Reasoning)
	}
	if row.Timings != nil {
		addFloat(&attrs, semconv.AttrAIGatewayTimingTotalMS, row.Timings.Total)
		addFloat(&attrs, semconv.AttrAIGatewayTimingLatencyMS, row.Timings.Latency)
	}
	if row.Location != nil {
		add(semconv.AttrAIGatewayRegion, row.Location.Region)
		add(semconv.AttrAIGatewayColo, row.Location.Colo)
	}
	addFloat(&attrs, semconv.AttrAIGatewayCost, row.Cost)
	addFloat(&attrs, semconv.AttrGenAILegacyCost, row.Cost)
	add(semconv.AttrAIGatewayCustomCost, safeScalar(row.CustomCost))
	add(semconv.AttrAIGatewayStep, optionalInt(row.Step))
	add(semconv.AttrAIGatewayFeedback, safeScalar(row.Feedback))
	add(semconv.AttrAIGatewayScore, safeScalar(row.Score))
	if len(row.Authentication) > 0 && string(row.Authentication) != "null" {
		add(semconv.AttrAIGatewayAuthenticationPresent, "true")
	}
	if row.Wholesale != nil {
		add(semconv.AttrAIGatewayWholesale, strconv.FormatBool(*row.Wholesale))
	}
	add(semconv.AttrAIGatewayBYOK, safeBoolean(row.BYOK))
	add(semconv.AttrAIGatewayUserAgent, capString(row.UserAgent, 512))
	dlp, hasDLP := parseDLPOutcome(row.DLPAction, row.DLPProfiles)
	if hasDLP {
		add(semconv.AttrAIGatewayDLPAction, dlp.Action)
		add(semconv.AttrAIGatewayDLPDirection, jsonStringArray(dlp.Directions))
		add(semconv.AttrAIGatewayDLPPolicyID, jsonStringArray(dlp.PolicyIDs))
		add(semconv.AttrAIGatewayDLPProfileID, jsonStringArray(dlp.ProfileIDs))
	}
	add(semconv.AttrAIGatewayMetadata, redactedJSON(row.Metadata, 4096))
	add(semconv.AttrAIGatewayGuardrails, redactedJSON(row.Guardrails, 4096))
	add(semconv.AttrAIGatewayDLPProfiles, redactedJSON(row.DLPProfiles, 4096))
	if c.cfg.AIGateway.CaptureBodies {
		add(semconv.AttrAIGatewayPrompts, redactedJSON(row.Prompts, int(c.cfg.AIGateway.MaxBodyBytes)))
	}
	var links []trace.Link
	var events []telemetry.SpanEvent
	var contentLogs []telemetry.LogRecord
	var nonJSONBodySides []string
	if c.cfg.AIGateway.CaptureBodies || c.cfg.AIGateway.LinkCallerTraces {
		path := "/accounts/" + url.PathEscape(c.cfg.Cloudflare.AccountID) + "/ai-gateway/gateways/" + url.PathEscape(gateway) + "/logs/" + url.PathEscape(row.ID)
		captured, err := c.fetchLog(ctx, path)
		if err != nil {
			return err
		}
		detail := captured.detail
		addInt(&attrs, semconv.AttrAIGatewayRequestSize, detail.RequestSize)
		addInt(&attrs, semconv.AttrAIGatewayResponseSize, detail.ResponseSize)
		if detail.RequestHeadComplete != nil {
			add(semconv.AttrAIGatewayRequestHeadComplete, strconv.FormatBool(*detail.RequestHeadComplete))
		}
		if detail.ResponseHeadComplete != nil {
			add(semconv.AttrAIGatewayResponseHeadComplete, strconv.FormatBool(*detail.ResponseHeadComplete))
		}
		add(semconv.AttrAIGatewayRequestContentType, detail.RequestContentType)
		if c.cfg.AIGateway.CaptureBodies {
			add(semconv.AttrAIGatewayRequestHead, safeHeaders(detail.RequestHead))
			add(semconv.AttrAIGatewayResponseHead, safeHeaders(detail.ResponseHead))
		}
		if c.cfg.AIGateway.LinkCallerTraces {
			links = traceLinks(detail.RequestHead)
		}
		if c.cfg.AIGateway.CaptureBodies {
			contentAttrs := make([]telemetry.Attr, 0, 4)
			for i, side := range []struct{ suffix, bodyKey, messageKey, truncatedKey string }{{"request", semconv.AttrAIGatewayRequestBody, semconv.AttrGenAIInputMessages, semconv.AttrAIGatewayRequestBodyTruncated}, {"response", semconv.AttrAIGatewayResponseBody, semconv.AttrGenAIOutputMessages, semconv.AttrAIGatewayResponseBodyTruncated}} {
				body := captured.bodies[i]
				if captured.unavailable[i] {
					key := semconv.AttrAIGatewayRequestBodyUnavailable
					if side.suffix == "response" {
						key = semconv.AttrAIGatewayResponseBodyUnavailable
					}
					missing := telemetry.Attr{Key: key, Value: "true"}
					attrs = append(attrs, missing)
					contentAttrs = append(contentAttrs, missing)
					continue
				}
				// Classify the complete body before capping: a truncated JSON
				// document is not an upstream non-JSON body. Never export opaque
				// non-JSON bytes through any content path.
				if !json.Valid(body) {
					key := semconv.AttrAIGatewayRequestBodyNonJSON
					if side.suffix == "response" {
						key = semconv.AttrAIGatewayResponseBodyNonJSON
					}
					attrs = append(attrs, telemetry.Attr{Key: key, Value: "true"})
					nonJSONBodySides = append(nonJSONBodySides, side.suffix)
					continue
				}
				capped, truncated := capBody(body, c.cfg.AIGateway.MaxBodyBytes)
				if side.suffix == "response" && !truncated {
					bodyUsage = resolveResponseUsage(&input, &output, &total, capped)
				}
				contentAttrs = append(contentAttrs, telemetry.Attr{Key: side.truncatedKey, Value: strconv.FormatBool(truncated)})
				contentBody := ""
				parsed := false
				if !truncated {
					if messages, ok := parseMessages(capped, side.suffix); ok {
						attr := telemetry.Attr{Key: side.messageKey, Value: messages}
						attrs = append(attrs, attr)
						contentAttrs = append(contentAttrs, attr)
						parsed = true
						if int64(len(messages)) <= c.cfg.AIGateway.MaxBodyBytes {
							contentBody = messages
						} else {
							// A parsed representation can expand beyond the raw body.
							// Keep the complete capped source JSON instead of cutting
							// the parsed JSON in the middle of a value.
							contentBody = string(capped)
						}
					}
				}
				if !parsed && len(capped) > 0 {
					contentBody = string(capped)
					contentAttrs = append(contentAttrs, telemetry.Attr{Key: side.bodyKey, Value: string(capped)})
				}
				if contentBody != "" {
					logAttrs := []telemetry.Attr{
						{Key: semconv.AttrAIGatewayName, Value: gateway},
						{Key: semconv.AttrAIGatewayLogID, Value: row.ID},
						{Key: semconv.AttrGenAIOperation, Value: operation},
					}
					logAttrs = append(logAttrs, telemetry.Attr{Key: semconv.AttrGenAIModel, Value: model})
					if provider != "" {
						logAttrs = append(logAttrs, telemetry.Attr{Key: semconv.AttrGenAIProvider, Value: provider})
					}
					logAttrs = append(logAttrs,
						telemetry.Attr{Key: semconv.AttrAIGatewayContentSide, Value: side.suffix},
						telemetry.Attr{Key: semconv.AttrAIGatewayContentLength, Value: strconv.Itoa(len(contentBody))},
						telemetry.Attr{Key: side.truncatedKey, Value: strconv.FormatBool(truncated)},
					)
					contentLogs = append(contentLogs, telemetry.LogRecord{
						Name:     semconv.EventGenAIContent,
						Body:     contentBody,
						At:       end,
						Severity: otellog.SeverityInfo,
						Attrs:    logAttrs,
					})
				}
			}
			events = append(events, telemetry.SpanEvent{Name: semconv.EventGenAIContent, At: end, Attrs: contentAttrs})
		}
	}
	addInt(&attrs, semconv.AttrGenAIInputTokens, input)
	addInt(&attrs, semconv.AttrGenAIOutputTokens, output)
	addInt(&attrs, semconv.AttrAIGatewayTotalTokens, total)
	if bodyUsage {
		add(semconv.AttrAIGatewayUsageSource, "response_body")
	}
	logAttrs := make([]telemetry.Attr, 0, len(attrs)+1)
	for _, attr := range attrs {
		if attr.Key != semconv.AttrGenAIInputMessages && attr.Key != semconv.AttrGenAIOutputMessages {
			logAttrs = append(logAttrs, attr)
		}
	}
	logAttrs = append(logAttrs, telemetry.Attr{Key: semconv.AttrAIGatewayCreatedAt, Value: end.Format(time.RFC3339Nano)})
	if err := out.LogEvent(ctx, semconv.EventAIGatewayRequest, "AI Gateway request", end, otellog.SeverityInfo, logAttrs...); err != nil {
		return err
	}
	name := operation + " " + model
	span := telemetry.SpanSpec{Name: name, Start: start, End: end, Kind: trace.SpanKindClient, Links: links, Events: events, Logs: contentLogs, Attrs: attrs}
	failed := (row.StatusCode != nil && *row.StatusCode >= 400) || (row.Success != nil && !*row.Success)
	if failed {
		span.Error = errors.New("AI Gateway request failed")
	}
	if err := out.Span(ctx, span); err != nil {
		return err
	}
	for _, side := range nonJSONBodySides {
		if err := out.Counter(ctx, semconv.MetricAIGatewayBodyNonJSON, 1, telemetry.Attr{Key: semconv.AttrAIGatewayBodySide, Value: side}); err != nil {
			return err
		}
	}
	dims := []telemetry.Attr{{Key: semconv.AttrAIGatewayName, Value: gateway}, {Key: semconv.AttrGenAIOperation, Value: operation}}
	if provider != "" {
		dims = append(dims, telemetry.Attr{Key: semconv.AttrGenAIProvider, Value: provider})
	}
	dims = append(dims, telemetry.Attr{Key: semconv.AttrGenAIModel, Value: model})
	statusClass := "unknown"
	if row.StatusCode != nil {
		statusClass = strconv.Itoa(*row.StatusCode/100) + "xx"
	}
	dims = append(dims, telemetry.Attr{Key: semconv.AttrStatusClass, Value: statusClass})
	if err := out.Counter(ctx, semconv.MetricAIGatewayRequests, 1, dims...); err != nil {
		return err
	}
	durationDims := append([]telemetry.Attr(nil), dims...)
	if failed {
		if err := out.Counter(ctx, semconv.MetricAIGatewayErrors, 1, dims...); err != nil {
			return err
		}
		errorType := "request_failed"
		if row.StatusCode != nil {
			errorType = strconv.Itoa(*row.StatusCode)
		}
		durationDims = append(durationDims, telemetry.Attr{Key: semconv.AttrErrorType, Value: errorType})
	}
	if err := out.Histogram(ctx, semconv.MetricGenAIDuration, *row.Duration/1000, durationDims...); err != nil {
		return err
	}
	if row.Cached != nil && *row.Cached {
		if err := out.Counter(ctx, semconv.MetricAIGatewayCacheHits, 1, dims...); err != nil {
			return err
		}
	}
	for _, token := range []struct {
		count              *int64
		counter, histogram string
	}{{input, semconv.MetricGenAIInputTokens, semconv.MetricGenAIInputOperationTokens}, {output, semconv.MetricGenAIOutputTokens, semconv.MetricGenAIOutputOperationTokens}} {
		if token.count != nil && *token.count >= 0 {
			if err := out.Counter(ctx, token.counter, float64(*token.count), dims...); err != nil {
				return err
			}
			if err := out.Histogram(ctx, token.histogram, float64(*token.count), dims...); err != nil {
				return err
			}
		}
	}
	if row.Usage != nil {
		for _, token := range []struct {
			count  *int64
			metric string
		}{{row.Usage.Cached, semconv.MetricGenAICacheReadTokens}, {row.Usage.Reasoning, semconv.MetricGenAIReasoningTokens}} {
			if token.count != nil && *token.count >= 0 {
				if err := out.Counter(ctx, token.metric, float64(*token.count), dims...); err != nil {
					return err
				}
			}
		}
	}
	if row.Cost != nil && *row.Cost >= 0 {
		if err := out.Counter(ctx, semconv.MetricAIGatewayCost, *row.Cost, dims...); err != nil {
			return err
		}
	}
	if hasDLP {
		directions := dlp.Directions
		if len(directions) == 0 {
			directions = []string{"other"}
		}
		for _, direction := range directions {
			dlpDims := []telemetry.Attr{
				{Key: semconv.AttrAIGatewayName, Value: gateway},
				{Key: semconv.AttrAIGatewayDLPAction, Value: dlp.Action},
				{Key: semconv.AttrAIGatewayDLPDirection, Value: direction},
			}
			if err := out.Counter(ctx, semconv.MetricAIGatewayDLPRequests, 1, dlpDims...); err != nil {
				return err
			}
		}
	}
	return nil
}

func addInt(attrs *[]telemetry.Attr, key string, n *int64) {
	if n != nil {
		*attrs = append(*attrs, telemetry.Attr{Key: key, Value: strconv.FormatInt(*n, 10)})
	}
}
func addFloat(attrs *[]telemetry.Attr, key string, n *float64) {
	if n != nil {
		*attrs = append(*attrs, telemetry.Attr{Key: key, Value: strconv.FormatFloat(*n, 'f', -1, 64)})
	}
}
func optionalInt(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}
func capString(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
func safeScalar(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}
func safeBoolean(raw json.RawMessage) string {
	if string(raw) == "true" || string(raw) == "false" {
		return string(raw)
	}
	return ""
}
func operationName(path string) string {
	path = "/" + strings.Trim(strings.ToLower(path), "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return "chat"
	case strings.HasSuffix(path, "/completions"):
		return "text_completion"
	case strings.HasSuffix(path, "/embeddings"):
		return "embeddings"
	default:
		return "other"
	}
}
func providerName(slug string) string {
	slug = strings.ToLower(slug)
	switch slug {
	case "google-ai-studio":
		return "gcp.gemini"
	case "google-vertex-ai":
		return "gcp.vertex_ai"
	case "azure-openai":
		return "azure.ai.openai"
	case "aws-bedrock":
		return "aws.bedrock"
	case "mistral":
		return "mistral_ai"
	case "xai":
		return "x_ai"
	default:
		return slug
	}
}
