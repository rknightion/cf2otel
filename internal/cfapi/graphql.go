package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var _ Client = (*HTTPClient)(nil)
var graphQLErrField = regexp.MustCompile(`(?i)(?:access to the field|not entitled to field)\s*['\"]([^'\"]+)['\"]`)

type FieldLimitError struct {
	Dataset       string
	Wanted, Limit int
}
type RetentionGapError struct {
	Dataset string
	Floor   time.Time
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

func (c *HTTPClient) graph(ctx context.Context, q string) (graphResponse, error) {
	var out graphResponse
	b, _ := json.Marshal(map[string]string{"query": q})
	raw, err := c.do(ctx, "POST", "/graphql", nil, b)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
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
			return fmt.Errorf("not entitled to field '%s'", match[1])
		}
		return errors.New("graphql query failed")
	}
	return nil
}
func (c *HTTPClient) settingsFor(ctx context.Context, r GraphQLRequest, refresh bool) (DatasetSettings, error) {
	var zero DatasetSettings
	key := string(r.Scope) + "/" + r.ScopeID + "/" + r.Dataset
	c.mu.Lock()
	entry, ok := c.settings[key]
	c.mu.Unlock()
	if ok && !refresh && time.Now().Before(entry.expires) {
		return entry.value, nil
	}
	q := fmt.Sprintf("{viewer{%s(%s){settings{%s{enabled availableFields maxNumberOfFields maxDuration notOlderThan maxPageSize}}}}}", scopeName(r.Scope), scopeFilter(r.Scope, r.ScopeID), r.Dataset)
	response, err := c.graph(ctx, q)
	if err != nil {
		return zero, err
	}
	if err = gqlErrors(response); err != nil {
		return zero, err
	}
	node, err := firstNode(response, r.Scope)
	if err != nil {
		return zero, err
	}
	var settings map[string]json.RawMessage
	if err = json.Unmarshal(node["settings"], &settings); err != nil {
		return zero, err
	}
	if err = json.Unmarshal(settings[r.Dataset], &zero); err != nil {
		return zero, err
	}
	c.mu.Lock()
	c.settings[key] = cachedSettings{zero, time.Now().Add(15 * time.Minute)}
	c.mu.Unlock()
	return zero, nil
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
		for _, prefix := range []string{"dimensions_", "sum_", "avg_", "uniq_", "quantiles_"} {
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
		return fmt.Errorf("dataset %s disabled", r.Dataset)
	}
	fields := intersect(r.WantedFields, s.AvailableFields)
	if len(fields) == 0 {
		return fmt.Errorf("dataset %s has no entitled wanted fields", r.Dataset)
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
			return fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", r.Dataset, start.Format(time.RFC3339), end.Format(time.RFC3339), limit)
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

func normalField(field string) string {
	for _, prefix := range []string{"dimensions_", "sum_", "avg_", "uniq_", "quantiles_"} {
		if strings.HasPrefix(field, prefix) {
			return strings.TrimSuffix(prefix, "_") + "." + strings.TrimPrefix(field, prefix)
		}
	}
	return field
}
