// Package cfapi is the only package permitted to call Cloudflare. It supports read requests only.
package cfapi

import (
	"context"
	"encoding/json"
	"net/url"
	"time"
)

type Scope string

const (
	AccountScope Scope = "account"
	ZoneScope    Scope = "zone"
)

type GraphQLRequest struct {
	Scope        Scope
	ScopeID      string
	Dataset      string
	WantedFields []string
	Filter       map[string]any
	From, To     time.Time
	Limit        int
}
type DatasetSettings struct {
	Enabled           bool     `json:"enabled"`
	AvailableFields   []string `json:"availableFields"`
	MaxNumberOfFields int      `json:"maxNumberOfFields"`
	MaxDuration       int64    `json:"maxDuration"`
	NotOlderThan      int64    `json:"notOlderThan"`
	MaxPageSize       int      `json:"maxPageSize"`
}
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}
type Gateway struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Client interface {
	Get(ctx context.Context, path string, query url.Values, out any) error
	Query(ctx context.Context, request GraphQLRequest, out any) error
	Accounts(ctx context.Context) ([]Account, error)
	Zones(ctx context.Context) ([]Zone, error)
	Gateways(ctx context.Context, accountID string) ([]Gateway, error)
}
type Page struct {
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		Page       int    `json:"page"`
		PerPage    int    `json:"per_page"`
		TotalCount int    `json:"total_count"`
		Cursor     string `json:"cursor"`
	} `json:"result_info"`
}
