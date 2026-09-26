package aigateway

import (
	"encoding/json"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// These filters are deliberately conservative. Unknown opaque metadata is not
// exported; only JSON containers whose keys can be inspected are retained.
func redactedJSON(raw json.RawMessage, limit int) string {
	if len(raw) == 0 || string(raw) == "null" || limit <= 0 {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	cleaned, ok := redact(v, 0)
	if !ok {
		return ""
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil || len(encoded) > limit {
		return ""
	}
	return string(encoded)
}
func redact(v any, depth int) (any, bool) {
	if depth > 8 {
		return nil, false
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, value := range x {
			lower := strings.ToLower(key)
			if len(key) > 64 || strings.Contains(key, "@") || strings.Contains(lower, "authorization") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") || strings.Contains(lower, "cookie") || strings.Contains(lower, "credential") {
				continue
			}
			cleaned, ok := redact(value, depth+1)
			if ok {
				out[key] = cleaned
			}
		}
		return out, true
	case []any:
		if len(x) > 64 {
			return nil, false
		}
		out := make([]any, 0, len(x))
		for _, value := range x {
			cleaned, ok := redact(value, depth+1)
			if ok {
				out = append(out, cleaned)
			}
		}
		return out, true
	case string:
		return "[REDACTED]", true
	case float64, bool, nil:
		return x, true
	default:
		return nil, false
	}
}

func safeHeaders(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var headers map[string]any
	if json.Unmarshal(raw, &headers) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return ""
		}
		headers = make(map[string]any)
		for _, line := range strings.Split(s, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if ok {
				headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	}
	allowed := make(map[string]string)
	for key, value := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		switch lower {
		case "content-type", "traceparent", "tracestate", "accept":
			if s, ok := value.(string); ok && len(s) <= 512 {
				allowed[lower] = s
			}
		}
	}
	if len(allowed) == 0 {
		return ""
	}
	b, _ := json.Marshal(allowed)
	return string(b)
}

func traceLinks(raw json.RawMessage) []trace.Link {
	safe := safeHeaders(raw)
	var headers map[string]string
	if json.Unmarshal([]byte(safe), &headers) != nil {
		return nil
	}
	parts := strings.Split(headers["traceparent"], "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[3]) != 2 {
		return nil
	}
	traceID, err := trace.TraceIDFromHex(parts[1])
	if err != nil {
		return nil
	}
	spanID, err := trace.SpanIDFromHex(parts[2])
	if err != nil {
		return nil
	}
	flags := trace.TraceFlags(0)
	if parts[3] == "01" {
		flags = trace.FlagsSampled
	} else if parts[3] != "00" {
		return nil
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: flags, Remote: true})
	if !sc.IsValid() {
		return nil
	}
	return []trace.Link{{SpanContext: sc}}
}

func capBody(body json.RawMessage, max int64) ([]byte, bool) {
	if max <= 0 {
		return nil, len(body) > 0
	}
	if int64(len(body)) > max {
		return body[:max], true
	}
	return body, false
}

// dlpFinding is one entry of the AI Gateway logs API's `dlp_profiles` array.
// A request can carry several findings, each naming several policy IDs.
type dlpFinding struct {
	Profile struct {
		ProfileID string `json:"profile_id"`
	} `json:"profile"`
	PolicyIDs []string `json:"policy_ids"`
	Check     string   `json:"check"`
}

// dlpOutcome summarizes a row's DLP findings. Directions, PolicyIDs and
// ProfileIDs are sorted, de-duplicated string slices; Directions is empty
// when the action fired with no findings.
type dlpOutcome struct {
	Action     string
	Directions []string
	PolicyIDs  []string
	ProfileIDs []string
}

// parseDLPOutcome reports whether the row carries a non-empty dlp_action and,
// if so, its mapped outcome. A missing, null or empty dlp_profiles parses
// cleanly to no findings.
func parseDLPOutcome(action string, profiles json.RawMessage) (dlpOutcome, bool) {
	action = strings.TrimSpace(action)
	if action == "" {
		return dlpOutcome{}, false
	}
	directions := make(map[string]struct{})
	policies := make(map[string]struct{})
	profileIDs := make(map[string]struct{})
	for _, finding := range parseDLPFindings(profiles) {
		directions[dlpDirection(finding.Check)] = struct{}{}
		for _, id := range finding.PolicyIDs {
			if id != "" {
				policies[id] = struct{}{}
			}
		}
		if finding.Profile.ProfileID != "" {
			profileIDs[finding.Profile.ProfileID] = struct{}{}
		}
	}
	return dlpOutcome{
		Action:     dlpAction(action),
		Directions: sortedStringSet(directions),
		PolicyIDs:  sortedStringSet(policies),
		ProfileIDs: sortedStringSet(profileIDs),
	}, true
}

// parseDLPFindings tolerates a missing, null, empty or unparseable
// dlp_profiles field by returning no findings rather than an error; the AI
// Gateway logs API shape for this field is otherwise unverified.
func parseDLPFindings(raw json.RawMessage) []dlpFinding {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var findings []dlpFinding
	if json.Unmarshal(raw, &findings) != nil {
		return nil
	}
	return findings
}

func dlpAction(action string) string {
	switch strings.ToUpper(action) {
	case "FLAG":
		return "flagged"
	case "BLOCK":
		return "blocked"
	default:
		return "other"
	}
}

func dlpDirection(check string) string {
	switch strings.ToLower(strings.TrimSpace(check)) {
	case "request":
		return "request"
	case "response":
		return "response"
	default:
		return "other"
	}
}

func sortedStringSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// jsonStringArray encodes a sorted, non-empty string slice as its JSON array
// attribute value, matching the existing gen_ai messages attribute
// convention. An empty slice yields "" so callers can skip the attribute.
func jsonStringArray(values []string) string {
	if len(values) == 0 {
		return ""
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// parseMessages accepts only recognized chat message shapes. Other JSON stays
// Cloudflare-prefixed on the opt-in content event.
func parseMessages(raw []byte, side string) (string, bool) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		var encoded string
		if json.Unmarshal(raw, &encoded) != nil || json.Unmarshal([]byte(encoded), &root) != nil {
			return "", false
		}
	}
	var source json.RawMessage
	if side == "request" {
		source = root["messages"]
	} else {
		var choices []struct {
			Message json.RawMessage `json:"message"`
		}
		if json.Unmarshal(root["choices"], &choices) == nil && len(choices) > 0 {
			source = choices[0].Message
		}
		if len(source) > 0 {
			source = []byte("[" + string(source) + "]")
		}
	}
	if len(source) == 0 {
		return "", false
	}
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(source, &messages) != nil || len(messages) == 0 {
		return "", false
	}
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		if m.Role != "system" && m.Role != "developer" && m.Role != "user" && m.Role != "assistant" && m.Role != "tool" {
			return "", false
		}
		var text string
		if json.Unmarshal(m.Content, &text) != nil {
			return "", false
		}
		out = append(out, map[string]any{"role": m.Role, "parts": []map[string]string{{"type": "text", "content": text}}})
	}
	encoded, err := json.Marshal(out)
	return string(encoded), err == nil
}
