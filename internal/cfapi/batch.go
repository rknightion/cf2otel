package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var _ GraphQLBatchQuerier = (*HTTPClient)(nil)

type preparedBatchSelection struct {
	alias, query          string
	request               GraphQLRequest
	fields, budget, limit int
}

// QueryBatch executes independent selections without joining their sampled rows.
// Entitlement failures, absent aliases and saturated arrays return no results.
func (c *HTTPClient) QueryBatch(ctx context.Context, selections []GraphQLBatchSelection) (map[string]json.RawMessage, error) {
	if len(selections) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	seen := map[string]bool{}
	first := selections[0].Request
	// Validate the entire envelope before any settings or data request.
	for _, s := range selections {
		if !identifier.MatchString(s.Alias) || strings.HasPrefix(s.Alias, "__") || seen[s.Alias] {
			return nil, errors.New("invalid or duplicate GraphQL batch alias")
		}
		seen[s.Alias] = true
		if err := validRequest(s.Request); err != nil {
			return nil, err
		}
		if s.Request.Scope != first.Scope || s.Request.ScopeID != first.ScopeID {
			return nil, errors.New("GraphQL batch requires the same scope and ID")
		}
	}
	var prepared []preparedBatchSelection
	for _, s := range selections {
		r := s.Request
		settings, err := c.settingsFor(ctx, r, false)
		if err != nil {
			return nil, err
		}
		if !settings.Enabled {
			return nil, &UnentitledError{Dataset: r.Dataset, Disabled: true}
		}
		fields := intersect(r.WantedFields, settings.AvailableFields)
		if len(r.WantedFields) == 0 || len(fields) != len(r.WantedFields) {
			return nil, fmt.Errorf("dataset %s batch fields unavailable or duplicated", r.Dataset)
		}
		if settings.MaxNumberOfFields > 0 && len(fields) > settings.MaxNumberOfFields {
			return nil, &FieldLimitError{r.Dataset, len(fields), settings.MaxNumberOfFields}
		}
		if settings.MaxDuration > 0 && r.To.Sub(r.From) > time.Duration(settings.MaxDuration)*time.Second {
			return nil, errors.New("GraphQL batch window exceeds dataset duration")
		}
		if settings.NotOlderThan > 0 {
			cutoff := time.Now().Add(-time.Duration(settings.NotOlderThan) * time.Second)
			if r.From.Before(cutoff) {
				return nil, &RetentionGapError{Dataset: r.Dataset, Floor: cutoff}
			}
		}
		limit := r.Limit
		if limit <= 0 {
			limit = 100
		}
		if settings.MaxPageSize > 0 && limit > settings.MaxPageSize {
			limit = settings.MaxPageSize
		}
		filter := map[string]any{}
		for k, v := range r.Filter {
			filter[k] = v
		}
		filter["datetime_geq"] = r.From.UTC().Format(time.RFC3339)
		filter["datetime_lt"] = r.To.UTC().Format(time.RFC3339)
		f, err := gqlValue(filter)
		if err != nil {
			return nil, err
		}
		query := fmt.Sprintf("%s:%s(limit:%d,filter:%s){%s}", s.Alias, r.Dataset, limit, f, selection(fields))
		prepared = append(prepared, preparedBatchSelection{s.Alias, query, r, len(fields), settings.MaxNumberOfFields, limit})
	}
	result := map[string]json.RawMessage{}
	for start := 0; start < len(prepared); {
		end, total, budget := start, 0, 0
		for end < len(prepared) {
			p := prepared[end]
			nextBudget := budget
			if p.budget > 0 && (nextBudget == 0 || p.budget < nextBudget) {
				nextBudget = p.budget
			}
			if end > start && nextBudget > 0 && total+p.fields > nextBudget {
				break
			}
			budget = nextBudget
			total += p.fields
			end++
		}
		queries := make([]string, 0, end-start)
		for _, p := range prepared[start:end] {
			queries = append(queries, p.query)
		}
		q := fmt.Sprintf("{viewer{%s(%s){%s}}}", scopeName(first.Scope), scopeFilter(first.Scope, first.ScopeID), strings.Join(queries, " "))
		response, err := c.graph(ctx, q)
		if err != nil {
			return nil, err
		}
		if err = gqlErrors(response); err != nil {
			return nil, err
		}
		node, err := firstNode(response, first.Scope)
		if err != nil {
			return nil, err
		}
		for _, p := range prepared[start:end] {
			raw, ok := node[p.alias]
			if !ok || len(raw) == 0 || raw[0] != '[' {
				return nil, errors.New("GraphQL batch response missing row array")
			}
			var rows []json.RawMessage
			if err = json.Unmarshal(raw, &rows); err != nil {
				return nil, err
			}
			if len(rows) >= p.limit {
				return nil, &SaturationError{Dataset: p.request.Dataset, From: p.request.From, To: p.request.To, Limit: p.limit}
			}
			result[p.alias] = raw
		}
		start = end
	}
	return result, nil
}
