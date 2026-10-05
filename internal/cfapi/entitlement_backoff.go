package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// settingsFor shares one decision across all aliases and collectors on this
// client. Only an explicit disabled or absent zone dataset gets the backoff TTL;
// enabled discovery metadata lives 45-75 minutes, jittered so keys cached
// together do not all re-probe in one burst against the GraphQL quota.
func (c *HTTPClient) settingsFor(ctx context.Context, r GraphQLRequest, refresh bool) (DatasetSettings, error) {
	var zero DatasetSettings
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	key := string(r.Scope) + "/" + r.ScopeID + "/" + r.Dataset
	// Coalesce discovery by key, without holding a mutex during HTTP or making
	// independent zone/dataset requests wait. Waiters retain context cancellation.
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		c.mu.Lock()
		entry, ok := c.settings[key]
		// Refresh renegotiates fields, never bypasses an active zone denial.
		denied := ok && r.Scope == ZoneScope && !entry.value.Enabled
		if ok && (!refresh || denied) && now().Before(entry.expires) {
			c.mu.Unlock()
			return entry.value, nil
		}
		if pending, busy := c.settingsPending[key]; busy {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-pending:
				continue
			}
		}
		if c.settingsPending == nil {
			c.settingsPending = make(map[string]chan struct{})
		}
		c.settingsPending[key] = make(chan struct{})
		c.mu.Unlock()
		break
	}
	defer func() {
		c.mu.Lock()
		close(c.settingsPending[key])
		delete(c.settingsPending, key)
		c.mu.Unlock()
	}()
	q := fmt.Sprintf("{viewer{%s(%s){settings{%s{enabled availableFields maxNumberOfFields maxDuration notOlderThan maxPageSize}}}}}", scopeName(r.Scope), scopeFilter(r.Scope, r.ScopeID), r.Dataset)
	response, err := c.graph(ctx, r.Scope, r.ScopeID, q)
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
	if settings == nil {
		return zero, errors.New("GraphQL settings missing object")
	}
	raw, present := settings[r.Dataset]
	if !present && r.Scope != ZoneScope {
		// Retain the original account-scope absence error contract.
		return zero, errors.New("GraphQL account dataset settings missing")
	}
	if present {
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(raw, &fields); err != nil {
			return zero, err
		}
		var enabled *bool
		if err = json.Unmarshal(fields["enabled"], &enabled); err != nil {
			return zero, err
		}
		if enabled == nil {
			return zero, errors.New("GraphQL dataset settings missing enabled")
		}
		if err = json.Unmarshal(raw, &zero); err != nil {
			return DatasetSettings{}, err
		}
		if zero.Enabled && zero.AvailableFields == nil {
			return DatasetSettings{}, errors.New("GraphQL enabled dataset settings missing availableFields")
		}
	}
	ttl := 45*time.Minute + rand.N(30*time.Minute) //nolint:gosec // Cache jitter, not a secret.
	if r.Scope == ZoneScope && !zero.Enabled {
		ttl = c.entitlementBackoff
		if ttl <= 0 {
			ttl = time.Hour
		} // Constructors also accept minimal test configs.
	}
	c.mu.Lock()
	c.settings[key] = cachedSettings{zero, now().Add(ttl)}
	c.mu.Unlock()
	return zero, nil
}
