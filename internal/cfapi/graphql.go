package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var _ Client = (*HTTPClient)(nil)
var graphQLErrField = regexp.MustCompile(`(?i)(?:access to the field|not entitled to field)\s*['\"]([^'\"]+)['\"]`)

// UnentitledError identifies an unavailable field or disabled dataset without
// exposing the upstream error message. Existing caller-visible text is retained.
type UnentitledError struct {
	Field, Dataset string
	Disabled       bool
}

func (e *UnentitledError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("not entitled to field '%s'", e.Field)
	}
	if e.Disabled {
		return fmt.Sprintf("dataset %s disabled", e.Dataset)
	}
	return fmt.Sprintf("dataset %s has no entitled wanted fields", e.Dataset)
}

type FieldLimitError struct {
	Dataset       string
	Wanted, Limit int
}
type RetentionGapError struct {
	Dataset string
	Floor   time.Time
}

// SaturationError reports a GraphQL query window whose rows reached the
// requested limit, so the result may be truncated and the window must be split.
type SaturationError struct {
	Dataset  string
	From, To time.Time
	Limit    int
}

func (e *SaturationError) Error() string {
	return fmt.Sprintf("GraphQL dataset %s window %s..%s saturated limit %d", e.Dataset, e.From.Format(time.RFC3339), e.To.Format(time.RFC3339), e.Limit)
}

// saturationMessage matches the SaturationError text when it reaches a caller
// as an untyped error. cfapi owns the message, so it alone parses it.
var saturationMessage = regexp.MustCompile(`(?i)graphql dataset (\w+) window (?:(.*?) )?saturated limit (\d*)`)

// AsSaturation returns the saturation error in err's chain. It matches a
// *SaturationError with errors.As first and falls back to the canonical
// message for errors that carry the text without the type.
func AsSaturation(err error) (*SaturationError, bool) {
	if err == nil {
		return nil, false
	}
	var sat *SaturationError
	if errors.As(err, &sat) {
		return sat, true
	}
	match := saturationMessage.FindStringSubmatch(err.Error())
	if match == nil {
		return nil, false
	}
	sat = &SaturationError{Dataset: match[1]}
	if from, to, ok := strings.Cut(match[2], ".."); ok {
		sat.From, _ = time.Parse(time.RFC3339, from)
		sat.To, _ = time.Parse(time.RFC3339, to)
	}
	sat.Limit, _ = strconv.Atoi(match[3])
	return sat, true
}

func (e *RetentionGapError) Error() string {
	return fmt.Sprintf("dataset %s retention gap: floor %s", e.Dataset, e.Floor.UTC().Format(time.RFC3339))
}

func (e *FieldLimitError) Error() string {
	return fmt.Sprintf("dataset %s needs %d fields, limit %d; no stable join key for split selections", e.Dataset, e.Wanted, e.Limit)
}

type graphResponse struct {
	Data struct {
		Viewer struct {
			Accounts []map[string]json.RawMessage `json:"accounts"`
			Zones    []map[string]json.RawMessage `json:"zones"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *HTTPClient) graphRaw(ctx context.Context, q string) ([]byte, error) {
	b, _ := json.Marshal(map[string]string{"query": q})
	return c.do(ctx, "POST", "/graphql", nil, b)
}
func (c *HTTPClient) graphDirect(ctx context.Context, q string) (graphResponse, error) {
	var out graphResponse
	raw, err := c.graphRaw(ctx, q)
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}
func scopeName(s Scope) string {
	if s == ZoneScope {
		return "zones"
	}
	return "accounts"
}
func scopeFilter(s Scope, id string) string {
	key := "accountTag"
	if s == ZoneScope {
		key = "zoneTag"
	}
	b, _ := json.Marshal(id)
	return "filter:{" + key + ":" + string(b) + "}"
}
func firstNode(r graphResponse, s Scope) (map[string]json.RawMessage, error) {
	nodes := r.Data.Viewer.Accounts
	if s == ZoneScope {
		nodes = r.Data.Viewer.Zones
	}
	if len(nodes) == 0 {
		return nil, errors.New("GraphQL scope not found")
	}
	return nodes[0], nil
}
func gqlErrors(r graphResponse) error {
	if len(r.Errors) > 0 {
		if match := graphQLErrField.FindStringSubmatch(r.Errors[0].Message); len(match) == 2 && identifier.MatchString(match[1]) {
			return &UnentitledError{Field: match[1]}
		}
		return errors.New("graphql query failed")
	}
	return nil
}

// DatasetSettings exposes entitlement metadata for the read-only drift canary.
// Scope IDs remain request-only and must never be written into the public spec.
func (c *HTTPClient) DatasetSettings(ctx context.Context, scope Scope, scopeID, dataset string) (DatasetSettings, error) {
	if scope != AccountScope && scope != ZoneScope {
		return DatasetSettings{}, errors.New("invalid GraphQL scope")
	}
	if scopeID == "" || !identifier.MatchString(dataset) {
		return DatasetSettings{}, errors.New("invalid GraphQL settings request")
	}
	return c.settingsFor(ctx, GraphQLRequest{Scope: scope, ScopeID: scopeID, Dataset: dataset}, false)
}
func validRequest(r GraphQLRequest) error {
	if r.Scope != AccountScope && r.Scope != ZoneScope {
		return errors.New("invalid GraphQL scope")
	}
	if !identifier.MatchString(r.Dataset) {
		return errors.New("invalid GraphQL dataset")
	}
	if r.ScopeID == "" {
		return errors.New("empty GraphQL scope ID")
	}
	for _, f := range append(append([]string{}, r.WantedFields...), r.JoinFields...) {
		for _, part := range strings.Split(f, ".") {
			if !identifier.MatchString(part) {
				return fmt.Errorf("invalid GraphQL field %q", f)
			}
		}
	}
	if !r.From.Before(r.To) {
		return errors.New("invalid GraphQL window")
	}
	return nil
}
func intersect(wanted, available []string) []string {
	if len(wanted) == 0 {
		wanted = make([]string, 0, len(available))
		for _, f := range available {
			wanted = append(wanted, normalField(f))
		}
	}
	set := map[string]bool{}
	for _, f := range available {
		set[strings.ToLower(f)] = true
		for _, prefix := range []string{"dimensions_", "sum_", "avg_", "max_", "uniq_", "quantiles_"} {
			if strings.HasPrefix(f, prefix) {
				set[strings.ToLower(strings.TrimSuffix(prefix, "_")+"."+strings.TrimPrefix(f, prefix))] = true
			}
		}
	}
	out := make([]string, 0, len(wanted))
	seen := map[string]bool{}
	for _, f := range wanted {
		k := strings.ToLower(f)
		if set[k] && !seen[k] {
			out = append(out, f)
			seen[k] = true
		}
	}
	return out
}
func selection(fields []string) string {
	type node map[string]node
	root := node{}
	for _, field := range fields {
		n := root
		parts := strings.Split(field, ".")
		for _, p := range parts {
			if n[p] == nil {
				n[p] = node{}
			}
			n = n[p]
		}
	}
	var render func(node) string
	render = func(n node) string {
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out []string
		for _, k := range keys {
			if len(n[k]) == 0 {
				out = append(out, k)
			} else {
				out = append(out, k+"{"+render(n[k])+"}")
			}
		}
		return strings.Join(out, " ")
	}
	return render(root)
}
func gqlValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		b, _ := json.Marshal(x)
		return string(b), nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case float64, int, int64:
		b, _ := json.Marshal(x)
		return string(b), nil
	case nil:
		return "null", nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			if !identifier.MatchString(k) {
				return "", fmt.Errorf("invalid filter key %q", k)
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			s, e := gqlValue(x[k])
			if e != nil {
				return "", e
			}
			parts = append(parts, k+":"+s)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	case []any:
		parts := make([]string, 0, len(x))
		for _, v := range x {
			s, e := gqlValue(v)
			if e != nil {
				return "", e
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	default:
		return "", fmt.Errorf("unsupported filter value %T", v)
	}
}
func (c *HTTPClient) Query(ctx context.Context, r GraphQLRequest, out any) error {
	if err := validRequest(r); err != nil {
		return err
	}
	settings, err := c.settingsFor(ctx, r, false)
	if err != nil {
		return err
	}
	return c.queryWithSettings(ctx, r, out, settings, false)
}
func (c *HTTPClient) queryWithSettings(ctx context.Context, r GraphQLRequest, out any, s DatasetSettings, renegotiated bool) error {
	if !s.Enabled {
		return &UnentitledError{Dataset: r.Dataset, Disabled: true}
	}
	fields := intersect(r.WantedFields, s.AvailableFields)
	if len(fields) == 0 {
		return &UnentitledError{Dataset: r.Dataset}
	}
	chunks, err := fieldChunks(fields, r.JoinFields, s.MaxNumberOfFields)
	if err != nil {
		return &FieldLimitError{r.Dataset, len(fields), s.MaxNumberOfFields}
	}

	from := r.From
	if s.NotOlderThan > 0 {
		cutoff := time.Now().Add(-time.Duration(s.NotOlderThan) * time.Second)
		if from.Before(cutoff) {
			return &RetentionGapError{Dataset: r.Dataset, Floor: cutoff}
		}
	}
	if !from.Before(r.To) {
		return json.Unmarshal([]byte("[]"), out)
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 100
	}
	if s.MaxPageSize > 0 && limit > s.MaxPageSize {
		limit = s.MaxPageSize
	}
	rows := make([]json.RawMessage, 0)
	for start := from; start.Before(r.To); {
		end := r.To
		if s.MaxDuration > 0 {
			max := start.Add(time.Duration(s.MaxDuration) * time.Second)
			if max.Before(end) {
				end = max
			}
		}
		filter := map[string]any{}
		for k, v := range r.Filter {
			filter[k] = v
		}
		filter["datetime_geq"] = start.UTC().Format(time.RFC3339)
		filter["datetime_lt"] = end.UTC().Format(time.RFC3339)
		f, e := gqlValue(filter)
		if e != nil {
			return e
		}
		var merged []json.RawMessage
		for chunkIndex, chunkFields := range chunks {
			q := fmt.Sprintf("{viewer{%s(%s){%s(limit:%d,filter:%s){%s}}}}", scopeName(r.Scope), scopeFilter(r.Scope, r.ScopeID), r.Dataset, limit, f, selection(chunkFields))
			response, e := c.graph(ctx, q)
			if e != nil {
				return e
			}
			if e = gqlErrors(response); e != nil {
				if !renegotiated && graphQLErrField.MatchString(e.Error()) {
					fresh, refreshErr := c.settingsFor(ctx, r, true)
					if refreshErr != nil {
						return refreshErr
					}
					return c.queryWithSettings(ctx, r, out, fresh, true)
				}
				return e
			}
			node, e := firstNode(response, r.Scope)
			if e != nil {
				return e
			}
			var part []json.RawMessage
			if e = json.Unmarshal(node[r.Dataset], &part); e != nil {
				return e
			}
			if chunkIndex == 0 {
				merged = part
			} else {
				merged, e = joinRows(merged, part, r.JoinFields)
				if e != nil {
					return e
				}
			}
		}
		if len(merged) >= limit {
			return &SaturationError{Dataset: r.Dataset, From: start, To: end, Limit: limit}
		}
		rows = append(rows, merged...)
		start = end
	}
	b, _ := json.Marshal(rows)
	return json.Unmarshal(b, out)
}

func fieldChunks(fields, join []string, max int) ([][]string, error) {
	if max <= 0 || len(fields) <= max {
		return [][]string{fields}, nil
	}
	if len(join) == 0 || len(join) >= max {
		return nil, errors.New("no join capacity")
	}
	entitled := map[string]bool{}
	for _, f := range fields {
		entitled[strings.ToLower(f)] = true
	}
	for _, f := range join {
		if !entitled[strings.ToLower(f)] {
			return nil, errors.New("join field unavailable")
		}
	}
	rest := make([]string, 0, len(fields))
	joined := map[string]bool{}
	for _, f := range join {
		joined[strings.ToLower(f)] = true
	}
	for _, f := range fields {
		if !joined[strings.ToLower(f)] {
			rest = append(rest, f)
		}
	}
	chunks := make([][]string, 0)
	for len(rest) > 0 {
		n := max - len(join)
		if n > len(rest) {
			n = len(rest)
		}
		chunk := append(append([]string{}, join...), rest[:n]...)
		chunks = append(chunks, chunk)
		rest = rest[n:]
	}
	return chunks, nil
}
func joinRows(left, right []json.RawMessage, fields []string) ([]json.RawMessage, error) {
	if len(left) != len(right) {
		return nil, errors.New("GraphQL field chunks have different row counts")
	}
	index := map[string]map[string]json.RawMessage{}
	for _, raw := range right {
		key, row, err := rowKey(raw, fields)
		if err != nil {
			return nil, err
		}
		if _, exists := index[key]; exists {
			return nil, errors.New("GraphQL field chunk has duplicate join key")
		}
		index[key] = row
	}
	out := make([]json.RawMessage, 0, len(left))
	seen := map[string]bool{}
	for _, raw := range left {
		key, row, err := rowKey(raw, fields)
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, errors.New("GraphQL field chunk has duplicate join key")
		}
		seen[key] = true
		other, ok := index[key]
		if !ok {
			return nil, errors.New("GraphQL field chunks have mismatched join keys")
		}
		for k, v := range other {
			if prior, exists := row[k]; exists {
				merged, err := mergeJSON(prior, v)
				if err != nil {
					return nil, err
				}
				row[k] = merged
			} else {
				row[k] = v
			}
		}
		b, _ := json.Marshal(row)
		out = append(out, b)
	}
	return out, nil
}
func rowKey(raw json.RawMessage, fields []string) (string, map[string]json.RawMessage, error) {
	var row map[string]json.RawMessage
	if err := json.Unmarshal(raw, &row); err != nil {
		return "", nil, err
	}
	values := make([]json.RawMessage, 0, len(fields))
	for _, field := range fields {
		part := row
		path := strings.Split(field, ".")
		var value json.RawMessage
		for i, key := range path {
			var ok bool
			value, ok = part[key]
			if !ok {
				return "", nil, fmt.Errorf("GraphQL join field %s missing", field)
			}
			if i < len(path)-1 {
				if err := json.Unmarshal(value, &part); err != nil {
					return "", nil, err
				}
			}
		}
		values = append(values, value)
	}
	b, _ := json.Marshal(values)
	return string(b), row, nil
}

func mergeJSON(left, right json.RawMessage) (json.RawMessage, error) {
	var a, b map[string]json.RawMessage
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		if string(left) != string(right) {
			return nil, errors.New("GraphQL chunks disagree on shared value")
		}
		return left, nil
	}
	for key, value := range b {
		if prior, ok := a[key]; ok {
			merged, err := mergeJSON(prior, value)
			if err != nil {
				return nil, err
			}
			a[key] = merged
		} else {
			a[key] = value
		}
	}
	return json.Marshal(a)
}

// Packing bounds limit physical envelope cost and pending generated storage, not
// analytics windows or source rows. Provider combination/cost acceptance is a
// separate constraint; any provider error still fails the whole envelope.
const (
	graphMaxNodes        = 8
	graphMaxBody         = 32 << 10
	graphAccumulate      = 250 * time.Millisecond
	graphMaxPending      = 256
	graphMaxPendingBytes = 8 << 20
)

type graphToken struct {
	text       string
	start, end int
}
type graphSelection struct {
	name, key                        string
	nameEnd, argsStart, argsEnd, end int
	children                         []graphSelection
}
type graphParser struct {
	q      string
	tokens []graphToken
	pos    int
}

// Only the generated anonymous grammar is recognized. In particular quoted
// delimiters never participate in balancing, and no source substring is
// reserialized. Unsupported or malformed input keeps the direct-provider path.
func graphTokens(q string) ([]graphToken, bool) {
	var tokens []graphToken
	for i := 0; i < len(q); {
		start := i
		ch := q[i]
		if ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' || ch == ',' {
			i++
			continue
		}
		switch {
		case ch == '"':
			i++
			for i < len(q) && q[i] != '"' {
				if q[i] == '\\' {
					i++
					if i == len(q) {
						return nil, false
					}
				}
				i++
			}
			if i == len(q) {
				return nil, false
			}
			i++
			var value string
			if json.Unmarshal([]byte(q[start:i]), &value) != nil {
				return nil, false
			}
		case ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z':
			i++
			for i < len(q) && (q[i] == '_' || q[i] >= 'A' && q[i] <= 'Z' || q[i] >= 'a' && q[i] <= 'z' || q[i] >= '0' && q[i] <= '9') {
				i++
			}
		case ch == '-' || ch >= '0' && ch <= '9':
			i++
			for i < len(q) && strings.ContainsRune("0123456789.eE+-", rune(q[i])) {
				i++
			}
			if _, err := strconv.ParseFloat(q[start:i], 64); err != nil {
				return nil, false
			}
		case strings.ContainsRune("{}():[]", rune(ch)):
			i++
		default:
			return nil, false
		}
		tokens = append(tokens, graphToken{q[start:i], start, i})
	}
	return tokens, true
}
func (p *graphParser) take(text string) bool {
	if p.pos >= len(p.tokens) || p.tokens[p.pos].text != text {
		return false
	}
	p.pos++
	return true
}
func (p *graphParser) value(depth int) bool {
	if depth > 64 || p.pos >= len(p.tokens) {
		return false
	}
	if p.take("{") {
		for !p.take("}") {
			if p.pos >= len(p.tokens) || !identifier.MatchString(p.tokens[p.pos].text) {
				return false
			}
			p.pos++
			if !p.take(":") || !p.value(depth+1) {
				return false
			}
		}
		return true
	}
	if p.take("[") {
		for !p.take("]") {
			if !p.value(depth + 1) {
				return false
			}
		}
		return true
	}
	token := p.tokens[p.pos].text
	if token[0] == '"' || token == "true" || token == "false" || token == "null" || token[0] == '-' || token[0] >= '0' && token[0] <= '9' {
		p.pos++
		return true
	}
	return false
}
func (p *graphParser) selections(depth int) ([]graphSelection, bool) {
	if depth > 64 || !p.take("{") {
		return nil, false
	}
	var nodes []graphSelection
	for !p.take("}") {
		if p.pos >= len(p.tokens) || !identifier.MatchString(p.tokens[p.pos].text) {
			return nil, false
		}
		name := p.tokens[p.pos]
		p.pos++
		key := name.text
		if p.take(":") {
			if p.pos >= len(p.tokens) || !identifier.MatchString(p.tokens[p.pos].text) {
				return nil, false
			}
			name = p.tokens[p.pos]
			p.pos++
		}
		node := graphSelection{name: name.text, key: key, nameEnd: name.end}
		if p.pos < len(p.tokens) && p.tokens[p.pos].text == "(" {
			node.argsStart = p.tokens[p.pos].start
			p.pos++
			for !p.take(")") {
				if p.pos >= len(p.tokens) || !identifier.MatchString(p.tokens[p.pos].text) {
					return nil, false
				}
				p.pos++
				if !p.take(":") || !p.value(depth+1) {
					return nil, false
				}
			}
			node.argsEnd = p.tokens[p.pos-1].end
		}
		if p.pos < len(p.tokens) && p.tokens[p.pos].text == "{" {
			var ok bool
			node.children, ok = p.selections(depth + 1)
			if !ok {
				return nil, false
			}
		}
		node.end = p.tokens[p.pos-1].end
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, false
	}
	return nodes, true
}
func graphLeaves(nodes []graphSelection) int {
	count := 0
	for _, node := range nodes {
		if len(node.children) == 0 {
			count++
		} else {
			count += graphLeaves(node.children)
		}
	}
	return count
}

type graphUnit struct {
	name, key, tail    string
	fields, fieldLimit int
}
type graphResult struct {
	response graphResponse
	err      error
}

// Keep provider errors in the original response contract: Query owns its one
// field renegotiation. No data from a failed envelope may escape projection.
type graphEnvelopeFailure struct{ response graphResponse }

func (*graphEnvelopeFailure) Error() string { return "graphql query failed" }
func graphEnvelopeError(response graphResponse) error {
	if len(response.Errors) == 0 {
		return nil
	}
	var failed graphResponse
	failed.Errors = response.Errors
	return &graphEnvelopeFailure{failed}
}

type graphChild struct {
	ctx                         context.Context
	stop                        context.CancelFunc
	query, scope, id, scopeArgs string
	settings                    bool
	units                       []graphUnit
	next, remaining             int
	born                        time.Time
	result                      chan graphResult
	response                    graphResponse
	done                        bool
}

func (c *HTTPClient) recognizeGraph(ctx context.Context, q string) (*graphChild, bool) {
	tokens, ok := graphTokens(q)
	if !ok {
		return nil, false
	}
	p := graphParser{q: q, tokens: tokens}
	root, ok := p.selections(0)
	if !ok || p.pos != len(tokens) || len(root) != 1 || root[0].name != "viewer" || root[0].key != "viewer" || root[0].argsEnd != 0 || len(root[0].children) != 1 {
		return nil, false
	}
	scope := root[0].children[0]
	if scope.key != scope.name || (scope.name != "zones" && scope.name != "accounts") || scope.argsEnd == 0 {
		return nil, false
	}
	args, ok := graphTokens(q[scope.argsStart:scope.argsEnd])
	if !ok || len(args) != 9 {
		return nil, false
	}
	filterKey := "accountTag"
	if scope.name == "zones" {
		filterKey = "zoneTag"
	}
	for i, text := range []string{"(", "filter", ":", "{", filterKey, ":", "", "}", ")"} {
		if i != 6 && args[i].text != text {
			return nil, false
		}
	}
	var id string
	if json.Unmarshal([]byte(args[6].text), &id) != nil || id == "" {
		return nil, false
	}
	child := &graphChild{ctx: ctx, query: q, scope: scope.name, id: id, scopeArgs: q[scope.argsStart:scope.argsEnd], result: make(chan graphResult, 1)}
	nodes := scope.children
	if len(nodes) == 1 && nodes[0].name == "settings" && nodes[0].key == "settings" && nodes[0].argsEnd == 0 {
		child.settings = true
		nodes = nodes[0].children
	}
	if len(nodes) == 0 {
		return nil, false
	}
	for _, node := range nodes {
		if len(node.children) == 0 || node.name == "settings" || child.settings && node.argsEnd != 0 {
			return nil, false
		}
		unit := graphUnit{name: node.name, key: node.key, tail: q[node.nameEnd:node.end], fields: graphLeaves(node.children)}
		if !child.settings {
			scopeKind := AccountScope
			if child.scope == "zones" {
				scopeKind = ZoneScope
			}
			c.mu.Lock()
			cached, present := c.settings[string(scopeKind)+"/"+id+"/"+node.name]
			c.mu.Unlock()
			if present {
				unit.fieldLimit = cached.value.MaxNumberOfFields
			}
		}
		child.units = append(child.units, unit)
	}
	child.remaining = len(child.units)
	return child, true
}
func (c *HTTPClient) graphSignal() {
	c.graphMu.Lock()
	wake := c.graphWake
	c.graphMu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
func (c *HTTPClient) graph(ctx context.Context, q string) (graphResponse, error) {
	if err := ctx.Err(); err != nil {
		return graphResponse{}, err
	}
	clock := graphWorkClock()
	child, ok := c.recognizeGraph(ctx, q)
	if !ok {
		return c.graphDirect(ctx, q)
	}
	c.graphMu.Lock()
	if c.graphChildren >= graphMaxPending || len(q) > graphMaxPendingBytes-c.graphBytes {
		c.graphMu.Unlock()
		return graphResponse{}, errors.New("GraphQL pending queue full")
	}
	child.ctx, child.stop = context.WithCancel(ctx)
	child.born = clock.Now()
	c.graphChildren++
	c.graphBytes += len(q)
	c.graphPending = append(c.graphPending, child)
	if c.graphWake == nil {
		c.graphWake = make(chan struct{}, 1)
	}
	if !c.graphRunning {
		c.graphRunning = true
		go c.runGraphQueue(clock)
	}
	c.graphMu.Unlock()
	stop := context.AfterFunc(ctx, c.graphSignal)
	defer stop()
	c.graphSignal()
	select {
	case result := <-child.result:
		return result.response, result.err
	case <-ctx.Done():
		return graphResponse{}, ctx.Err()
	}
}

type graphPart struct {
	child                 *graphChild
	index                 int
	scopeAlias, nodeAlias string
}

func graphCompatible(a, b *graphChild) bool {
	return a.scope == b.scope && a.settings == b.settings && (a.scope == "zones" || a.id == b.id)
}
func graphBodySize(q string) int { b, _ := json.Marshal(map[string]string{"query": q}); return len(b) }
func renderGraph(parts []graphPart) (string, []graphPart) {
	aliases := map[string]string{}
	scopes := map[string][]int{}
	var order []string
	for i := range parts {
		part := &parts[i]
		id := part.child.id
		alias, ok := aliases[id]
		if !ok {
			alias = fmt.Sprintf("cfpack_s%d", len(order))
			aliases[id] = alias
			order = append(order, id)
		}
		part.scopeAlias = alias
		part.nodeAlias = fmt.Sprintf("cfpack_n%d", i)
		scopes[id] = append(scopes[id], i)
	}
	var out strings.Builder
	out.WriteString("{viewer{")
	for _, id := range order {
		indexes := scopes[id]
		child := parts[indexes[0]].child
		out.WriteString(aliases[id] + ":" + child.scope + child.scopeArgs + "{")
		for _, i := range indexes {
			part := parts[i]
			unit := part.child.units[part.index]
			if child.settings {
				out.WriteString(part.nodeAlias + ":settings{" + part.nodeAlias + "_d:" + unit.name + unit.tail + "} ")
			} else {
				out.WriteString(part.nodeAlias + ":" + unit.name + unit.tail + " ")
			}
		}
		out.WriteString("}")
	}
	out.WriteString("}}")
	return out.String(), parts
}

// Selection is oldest-first, with a per-scope field ledger. Unknown metadata
// never grants permission to combine analytics fields. Original independent
// selections, including batch aliases, are the only partition boundaries.
func (c *HTTPClient) graphPlan() ([]graphPart, string, bool) {
	oldest := c.graphPending[0]
	var parts []graphPart
	fields := map[string]int{}
	limits := map[string]int{}
	ids := map[string]bool{}
	for _, child := range c.graphPending {
		if !graphCompatible(oldest, child) {
			continue
		}
		for i := child.next; i < len(child.units); i++ {
			unit := child.units[i]
			if !child.settings && unit.fieldLimit <= 0 {
				if len(parts) == 0 {
					return []graphPart{{child: child, index: i}}, "", true
				}
				break
			}
			if len(parts) == graphMaxNodes || !ids[child.id] && len(ids) >= 10 {
				break
			}
			limit := unit.fieldLimit
			if limits[child.id] > 0 && (limit <= 0 || limits[child.id] < limit) {
				limit = limits[child.id]
			}
			if !child.settings && fields[child.id]+unit.fields > limit {
				if len(parts) == 0 {
					return []graphPart{{child: child, index: i}}, "", true
				}
				break
			}
			candidate := append(append([]graphPart(nil), parts...), graphPart{child: child, index: i})
			q, mapped := renderGraph(candidate)
			if graphBodySize(q) > graphMaxBody {
				if len(parts) == 0 {
					return []graphPart{{child: child, index: i}}, "", true
				}
				break
			}
			parts = mapped
			ids[child.id] = true
			fields[child.id] += unit.fields
			limits[child.id] = limit
		}
		if len(parts) == graphMaxNodes {
			break
		}
	}
	q, parts := renderGraph(parts)
	return parts, q, false
}
func (c *HTTPClient) finishGraphChild(child *graphChild, response graphResponse, err error) {
	if child.done {
		return
	}
	child.done = true
	child.stop()
	c.graphChildren--
	c.graphBytes -= len(child.query)
	child.result <- graphResult{response, err}
}
func (c *HTTPClient) pruneGraphQueue() {
	kept := c.graphPending[:0]
	for _, child := range c.graphPending {
		if err := child.ctx.Err(); err != nil {
			c.finishGraphChild(child, graphResponse{}, err)
		}
		if !child.done && child.next < len(child.units) {
			kept = append(kept, child)
		}
	}
	clear(c.graphPending[len(kept):])
	c.graphPending = kept
}
func (c *HTTPClient) runGraphQueue(clock AdmissionClock) {
	for {
		c.graphMu.Lock()
		c.pruneGraphQueue()
		if len(c.graphPending) == 0 {
			c.graphRunning = false
			c.graphMu.Unlock()
			return
		}
		oldest := c.graphPending[0]
		parts, q, direct := c.graphPlan()
		delay := oldest.born.Add(graphAccumulate).Sub(clock.Now())
		if delay > 0 && len(parts) < graphMaxNodes {
			wake := c.graphWake
			c.graphMu.Unlock()
			timer := clock.NewTimer(delay)
			select {
			case <-wake:
				timer.Stop()
			case <-timer.C():
			}
			continue
		}
		// Singleton fast path keeps existing wire fixtures unchanged. Partitioned
		// singletons use a wrapper with the same original subtree and response keys.
		singleton := len(parts) > 0
		child := parts[0].child
		for _, part := range parts {
			if part.child != child {
				singleton = false
			}
		}
		whole := singleton && child.next == 0 && len(parts) == len(child.units)
		if whole {
			q = child.query
			direct = true
		}
		if direct && !whole {
			unit := child.units[parts[0].index]
			node := unit.key + ":" + unit.name + unit.tail
			if child.settings {
				node = "settings{" + node + "}"
			}
			q = "{viewer{" + child.scope + child.scopeArgs + "{" + node + "}}}"
		}
		for _, part := range parts {
			part.child.next++
		}
		c.graphMu.Unlock()
		notice := &graphAdmissionNotice{ready: make(chan struct{})}
		go c.deliverGraphParts(parts, q, direct, whole, notice)
		// Register physical admission in sealing order, but never serialize
		// network exchanges or block independent settings behind a slow owner.
		<-notice.ready
	}
}
func (c *HTTPClient) deliverGraphParts(parts []graphPart, q string, direct, whole bool, notice *graphAdmissionNotice) {
	defer notice.signal()
	var original graphResponse
	projected, err := c.executeGraphParts(parts, q, direct, whole, &original, notice)
	c.graphMu.Lock()
	for i, part := range parts {
		child := part.child
		if child.done {
			continue
		}
		if child.ctx.Err() != nil {
			c.finishGraphChild(child, graphResponse{}, child.ctx.Err())
			continue
		}
		if err != nil {
			var failed *graphEnvelopeFailure
			if errors.As(err, &failed) {
				c.finishGraphChild(child, failed.response, nil)
			} else {
				c.finishGraphChild(child, graphResponse{}, err)
			}
			continue
		}
		if whole {
			c.finishGraphChild(child, original, nil)
			continue
		}
		node := projected[i]
		target := child.response.Data.Viewer.Accounts
		if child.scope == "zones" {
			target = child.response.Data.Viewer.Zones
		}
		if len(target) == 0 {
			target = []map[string]json.RawMessage{{}}
		}
		if child.settings {
			var metadata map[string]json.RawMessage
			if len(target[0]["settings"]) > 0 {
				_ = json.Unmarshal(target[0]["settings"], &metadata)
			}
			if metadata == nil {
				metadata = map[string]json.RawMessage{}
			}
			for key, value := range node {
				metadata[key] = append(json.RawMessage(nil), value...)
			}
			target[0]["settings"], _ = json.Marshal(metadata)
		} else {
			for key, value := range node {
				target[0][key] = append(json.RawMessage(nil), value...)
			}
		}
		if child.scope == "zones" {
			child.response.Data.Viewer.Zones = target
		} else {
			child.response.Data.Viewer.Accounts = target
		}
		child.remaining--
		if child.remaining == 0 {
			c.finishGraphChild(child, child.response, nil)
		}
	}
	c.graphMu.Unlock()
	c.graphSignal()
}
func (c *HTTPClient) executeGraphParts(parts []graphPart, q string, direct, whole bool, original *graphResponse, notice *graphAdmissionNotice) ([]map[string]json.RawMessage, error) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), graphAdmissionKey{}, notice))
	defer cancel()
	// An envelope is independent of any one child's cancellation/deadline. The
	// last live participant cancels pending admission, retry delay and exchange.
	live := map[*graphChild]bool{}
	for _, part := range parts {
		live[part.child] = true
	}
	var stops []func() bool
	check := func() {
		for child := range live {
			if child.ctx.Err() == nil {
				return
			}
		}
		cancel()
	}
	for child := range live {
		stops = append(stops, context.AfterFunc(child.ctx, check))
	}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	check()
	projected := make([]map[string]json.RawMessage, len(parts))
	if direct {
		response, err := c.graphDirect(ctx, q)
		if err != nil {
			return nil, err
		}
		if err = graphEnvelopeError(response); err != nil {
			return nil, err
		}
		if whole {
			*original = response
			return nil, nil
		}
		scope := AccountScope
		if parts[0].child.scope == "zones" {
			scope = ZoneScope
		}
		node, err := firstNode(response, scope)
		if err != nil {
			return nil, err
		}
		for i, part := range parts {
			original := node
			if part.child.settings {
				if err = json.Unmarshal(node["settings"], &original); err != nil || original == nil {
					return nil, errors.New("GraphQL settings missing object")
				}
			}
			projected[i] = map[string]json.RawMessage{}
			key := part.child.units[part.index].key
			if value, present := original[key]; present {
				projected[i][key] = value
			}
		}
		return projected, nil
	}
	raw, err := c.graphRaw(ctx, q)
	if err != nil {
		return nil, err
	}
	var response graphResponse
	if err = json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	if err = graphEnvelopeError(response); err != nil {
		return nil, err
	}
	var wire struct {
		Data struct {
			Viewer map[string]json.RawMessage `json:"viewer"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	scopes := map[string]map[string]json.RawMessage{}
	for i, part := range parts {
		node, present := scopes[part.scopeAlias]
		if !present {
			var nodes []map[string]json.RawMessage
			if json.Unmarshal(wire.Data.Viewer[part.scopeAlias], &nodes) != nil || len(nodes) != 1 || nodes[0] == nil {
				return nil, errors.New("GraphQL packed scope missing or malformed")
			}
			node = nodes[0]
			scopes[part.scopeAlias] = node
		}
		key := part.child.units[part.index].key
		projected[i] = map[string]json.RawMessage{}
		if part.child.settings {
			var settings map[string]json.RawMessage
			if json.Unmarshal(node[part.nodeAlias], &settings) != nil || settings == nil {
				return nil, errors.New("GraphQL settings missing object")
			}
			// Absent zone metadata is a legitimate disabled decision, not an alias
			// decoder failure. Account absence stays owned by settingsFor.
			if value, present := settings[part.nodeAlias+"_d"]; present {
				projected[i][key] = value
			}
		} else if value, present := node[part.nodeAlias]; present {
			projected[i][key] = value
		}
	}
	return projected, nil
}

func normalField(field string) string {
	for _, prefix := range []string{"dimensions_", "sum_", "avg_", "max_", "uniq_", "quantiles_"} {
		if strings.HasPrefix(field, prefix) {
			return strings.TrimSuffix(prefix, "_") + "." + strings.TrimPrefix(field, prefix)
		}
	}
	return field
}
