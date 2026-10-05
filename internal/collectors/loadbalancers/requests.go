package loadbalancers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const requestsDataset = "loadBalancingRequestsAdaptiveGroups"

var requestsFields = []string{"count", "dimensions.selectedPoolName"}

type settingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

// requests returns a replaceable, trailing-window count, not a cumulative
// counter: snapshot polls may overlap and have no delivery checkpoint. Never
// infer request volume by counting raw Adaptive rows or multiplying sample rates.
func (c *healthCollector) requests(ctx context.Context, pools []pool, budget int) ([]telemetry.GaugePoint, error) {
	if len(pools) == 0 {
		return nil, nil
	}
	reader, ok := c.api.(settingsReader)
	if !ok {
		return nil, fmt.Errorf("load balancer requests require dataset settings")
	}
	query, ok := c.api.(cfapi.GraphQLBatchQuerier)
	if !ok {
		return nil, fmt.Errorf("load balancer requests require strict selection client")
	}
	zones, err := c.api.Zones(ctx)
	if err != nil {
		return nil, fmt.Errorf("load balancer zone discovery failed")
	}
	if len(zones) > 1000 {
		return nil, fmt.Errorf("load balancer zone discovery bound exhausted")
	}
	allowed := map[string]bool{}
	for _, id := range c.zones {
		allowed[id] = true
	}
	known := map[string]bool{}
	for _, p := range pools {
		known[p.Name] = true
	}
	sums := map[string]float64{}
	to := time.Now().UTC().Truncate(time.Second)
	from := to.Add(-c.interval).Truncate(time.Second)
	seen := map[string]bool{}
	for _, zone := range zones {
		if zone.Account.ID != c.account || len(allowed) > 0 && !allowed[zone.ID] {
			continue
		}
		if zone.ID == "" || seen[zone.ID] {
			return nil, fmt.Errorf("load balancer zone discovery invalid identity")
		}
		seen[zone.ID] = true
		s, err := reader.DatasetSettings(ctx, cfapi.ZoneScope, zone.ID, requestsDataset)
		if denied(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load balancer requests settings read failed")
		}
		if !s.Enabled {
			continue
		}
		fields := make([]string, 0, len(requestsFields))
		for _, wanted := range requestsFields {
			for _, available := range s.AvailableFields {
				if strings.EqualFold(strings.TrimSpace(available), wanted) || strings.EqualFold(strings.TrimSpace(available), strings.ReplaceAll(wanted, ".", "_")) {
					fields = append(fields, wanted)
					break
				}
			}
		}
		// Partial grouping is not traffic attribution. Missing entitlement is a no-op.
		if len(fields) != len(requestsFields) {
			continue
		}
		if s.MaxNumberOfFields < len(fields) || s.MaxPageSize < 1 || s.MaxDuration < 1 || s.NotOlderThan < 1 {
			return nil, fmt.Errorf("load balancer requests settings have unusable limits")
		}
		if to.Sub(from) > time.Duration(s.MaxDuration)*time.Second || time.Since(from) >= time.Duration(s.NotOlderThan)*time.Second {
			return nil, fmt.Errorf("load balancer requests window exceeds duration or retention")
		}
		limit := min(s.MaxPageSize, 10000)
		result, err := query.QueryBatch(ctx, []cfapi.GraphQLBatchSelection{{Alias: "pool_requests", Request: cfapi.GraphQLRequest{
			Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: requestsDataset, WantedFields: fields, From: from, To: to, Limit: limit,
		}}})
		if denied(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load balancer requests query failed")
		}
		var rows []struct {
			Count      *float64 `json:"count"`
			Dimensions struct {
				PoolName *string `json:"selectedPoolName"`
			} `json:"dimensions"`
		}
		if err := json.Unmarshal(result["pool_requests"], &rows); err != nil || rows == nil || len(rows) >= limit {
			return nil, fmt.Errorf("load balancer requests invalid or saturated row array")
		}
		for _, row := range rows {
			if row.Count == nil || math.IsNaN(*row.Count) || math.IsInf(*row.Count, 0) || *row.Count < 0 || math.Trunc(*row.Count) != *row.Count || *row.Count > 1<<53 {
				return nil, fmt.Errorf("load balancer requests invalid count")
			}
			if row.Dimensions.PoolName == nil || !validName(*row.Dimensions.PoolName) {
				return nil, fmt.Errorf("load balancer requests missing or invalid pool name")
			}
			name := *row.Dimensions.PoolName
			// Historical/unresolved names never become new public labels.
			if !known[name] {
				name = "other"
			}
			sums[name] += *row.Count
			if sums[name] > 1<<53 {
				return nil, fmt.Errorf("load balancer requests count exceeds exact numeric range")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if budget < 1 {
		return nil, nil
	}
	names := make([]string, 0, len(sums))
	for name := range sums {
		if name != "other" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var points []telemetry.GaugePoint
	other, hasOther := sums["other"]
	for _, name := range names {
		if len(points) < budget-1 {
			points = append(points, point(name, sums[name]))
			continue
		}
		other += sums[name]
		hasOther = true
	}
	if hasOther {
		if other > 1<<53 {
			return nil, fmt.Errorf("load balancer requests remainder exceeds exact numeric range")
		}
		points = append(points, point("other", other))
	}
	return points, nil
}
func denied(err error) bool {
	var denial *cfapi.UnentitledError
	return errors.As(err, &denial)
}
