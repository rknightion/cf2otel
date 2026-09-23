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

type logs struct {
	cfg *config.Config
	api cfapi.Client
}

func NewLogs(cfg *config.Config, api cfapi.Client) *logs { return &logs{cfg: cfg, api: api} }
func (*logs) Name() string                               { return "aigateway.logs" }
func (*logs) DefaultInterval() time.Duration             { return 5 * time.Minute }
func (*logs) Lag() time.Duration                         { return time.Minute }

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
	if !from.Before(to) || c.cfg == nil || c.api == nil {
		return time.Time{}, errors.New("aigateway logs: invalid window or dependencies")
	}
	all := make([]namedRow, 0)
	seen := make(map[string]bool)
	for _, gateway := range c.cfg.AIGateway.Gateways {
		if gateway == "" {
			return time.Time{}, errors.New("aigateway logs: empty gateway")
		}
		path := "/accounts/" + url.PathEscape(c.cfg.Cloudflare.AccountID) + "/ai-gateway/gateways/" + url.PathEscape(gateway) + "/logs"
		for pageNumber := 1; pageNumber <= 10000; pageNumber++ {
			if err := ctx.Err(); err != nil {
				return time.Time{}, err
			}
			query := url.Values{"page": {strconv.Itoa(pageNumber)}, "per_page": {strconv.Itoa(logPageSize)}, "order_by": {"created_at"}, "order_by_direction": {"asc"}, "start_date": {from.UTC().Format(time.RFC3339Nano)}, "end_date": {to.UTC().Format(time.RFC3339Nano)}}
			var page []logRow
			if err := c.api.Get(ctx, path, query, &page); err != nil {
				return time.Time{}, fmt.Errorf("aigateway logs page %d: %w", pageNumber, err)
			}
			pastTo := false
			for _, row := range page {
				if row.ID == "" || row.CreatedAt.IsZero() {
					return time.Time{}, fmt.Errorf("aigateway logs page %d: missing id or created_at", pageNumber)
				}
				if !row.CreatedAt.Before(to) {
					pastTo = true
					continue
				}
				if row.CreatedAt.Before(from) {
					continue
				}
				key := gateway + "\x00" + row.ID
				if seen[key] {
					continue
				}
				seen[key] = true
				all = append(all, namedRow{gateway, row})
			}
			// Get unwraps result and does not expose result_info.per_page. The
			// server may cap per_page below our requested size, so only an empty
			// page (or crossing to) proves that pagination is complete.
			if pastTo || len(page) == 0 {
				break
			}
			if pageNumber == 10000 {
				return time.Time{}, errors.New("aigateway logs: page limit reached")
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.row.CreatedAt.Equal(b.row.CreatedAt) {
			return a.row.ID < b.row.ID
		}
		return a.row.CreatedAt.Before(b.row.CreatedAt)
	})
	for _, item := range all {
		if err := c.emit(ctx, item.gateway, item.row, out); err != nil {
			return time.Time{}, err
		}
	}
	return to, nil
}

func (c *logs) emit(ctx context.Context, gateway string, row logRow, out telemetry.Emitter) error {
	if row.Duration == nil || *row.Duration < 0 || *row.Duration > float64((24*time.Hour)/time.Millisecond) {
		return errors.New("aigateway logs: invalid duration")
	}
	end := row.CreatedAt
	start := end.Add(-time.Duration(*row.Duration * float64(time.Millisecond)))
	operation := operationName(row.Path)
	provider := providerName(row.Provider)
	attrs := []telemetry.Attr{{Key: semconv.AttrAIGatewayName, Value: gateway}, {Key: semconv.AttrAIGatewayLogID, Value: row.ID}, {Key: semconv.AttrGenAIOperation, Value: operation}}
	add := func(key, value string) {
		if value != "" {
			attrs = append(attrs, telemetry.Attr{Key: key, Value: value})
		}
	}
	add(semconv.AttrAIGatewayEventID, row.EventID)
	add(semconv.AttrAIGatewayProvider, row.Provider)
	add(semconv.AttrGenAIProvider, provider)
	add(semconv.AttrGenAIModel, row.Model)
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
	if row.Usage != nil {
		if row.Usage.Input != nil {
			input = row.Usage.Input
		}
		if row.Usage.Output != nil {
			output = row.Usage.Output
		}
		addInt(&attrs, semconv.AttrAIGatewayTotalTokens, row.Usage.Total)
		addInt(&attrs, semconv.AttrGenAICacheReadTokens, row.Usage.Cached)
		addInt(&attrs, semconv.AttrGenAIReasoningTokens, row.Usage.Reasoning)
	}
	addInt(&attrs, semconv.AttrGenAIInputTokens, input)
	addInt(&attrs, semconv.AttrGenAIOutputTokens, output)
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
	add(semconv.AttrAIGatewayDLPAction, row.DLPAction)
	add(semconv.AttrAIGatewayMetadata, redactedJSON(row.Metadata, 4096))
	add(semconv.AttrAIGatewayGuardrails, redactedJSON(row.Guardrails, 4096))
	add(semconv.AttrAIGatewayDLPProfiles, redactedJSON(row.DLPProfiles, 4096))
	if c.cfg.AIGateway.CaptureBodies {
		add(semconv.AttrAIGatewayPrompts, redactedJSON(row.Prompts, int(c.cfg.AIGateway.MaxBodyBytes)))
	}
	var links []trace.Link
	var events []telemetry.SpanEvent
	if c.cfg.AIGateway.CaptureBodies || c.cfg.AIGateway.LinkCallerTraces {
		path := "/accounts/" + url.PathEscape(c.cfg.Cloudflare.AccountID) + "/ai-gateway/gateways/" + url.PathEscape(gateway) + "/logs/" + url.PathEscape(row.ID)
		var detail logRow
		if err := c.api.Get(ctx, path, nil, &detail); err != nil {
			return fmt.Errorf("aigateway log detail: %w", err)
		}
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
			rawGetter, ok := c.api.(cfapi.RawGetter)
			if !ok {
				return errors.New("aigateway API does not support raw body reads")
			}
			contentAttrs := make([]telemetry.Attr, 0, 4)
			for _, side := range []struct{ suffix, bodyKey, messageKey, truncatedKey string }{{"request", semconv.AttrAIGatewayRequestBody, semconv.AttrGenAIInputMessages, semconv.AttrAIGatewayRequestBodyTruncated}, {"response", semconv.AttrAIGatewayResponseBody, semconv.AttrGenAIOutputMessages, semconv.AttrAIGatewayResponseBodyTruncated}} {
				var body json.RawMessage
				if err := rawGetter.GetRaw(ctx, path+"/"+side.suffix, nil, &body); err != nil {
					var httpErr *cfapi.HTTPError
					if side.suffix == "response" && errors.As(err, &httpErr) && httpErr.Status == 404 && httpErr.Code == 7002 {
						missing := telemetry.Attr{Key: semconv.AttrAIGatewayResponseBodyUnavailable, Value: "true"}
						attrs = append(attrs, missing)
						contentAttrs = append(contentAttrs, missing)
						continue
					}
					return fmt.Errorf("aigateway %s body: %w", side.suffix, err)
				}
				capped, truncated := capBody(body, c.cfg.AIGateway.MaxBodyBytes)
				contentAttrs = append(contentAttrs, telemetry.Attr{Key: side.truncatedKey, Value: strconv.FormatBool(truncated)})
				if !truncated {
					if messages, ok := parseMessages(capped, side.suffix); ok {
						attr := telemetry.Attr{Key: side.messageKey, Value: messages}
						attrs = append(attrs, attr)
						contentAttrs = append(contentAttrs, attr)
						continue
					}
				}
				if len(capped) > 0 {
					contentAttrs = append(contentAttrs, telemetry.Attr{Key: side.bodyKey, Value: string(capped)})
				}
			}
			events = append(events, telemetry.SpanEvent{Name: semconv.EventGenAIContent, At: end, Attrs: contentAttrs})
		}
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
	name := operation
	if row.Model != "" {
		name += " " + row.Model
	}
	span := telemetry.SpanSpec{Name: name, Start: start, End: end, Kind: trace.SpanKindClient, Links: links, Events: events, Attrs: attrs}
	failed := (row.StatusCode != nil && *row.StatusCode >= 400) || (row.Success != nil && !*row.Success)
	if failed {
		span.Error = errors.New("AI Gateway request failed")
	}
	if err := out.Span(ctx, span); err != nil {
		return err
	}
	dims := []telemetry.Attr{{Key: semconv.AttrAIGatewayName, Value: gateway}, {Key: semconv.AttrGenAIOperation, Value: operation}}
	if provider != "" {
		dims = append(dims, telemetry.Attr{Key: semconv.AttrGenAIProvider, Value: provider})
	}
	if row.Model != "" {
		dims = append(dims, telemetry.Attr{Key: semconv.AttrGenAIModel, Value: row.Model})
	}
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
