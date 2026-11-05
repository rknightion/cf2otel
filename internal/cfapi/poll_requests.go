package cfapi

import (
	"context"
	"sync"
	"time"
)

// RequestRole describes bounded request purposes, never paths or resource IDs.
type RequestRole string

const (
	RequestRoleOther  RequestRole = "other"
	RequestRoleList   RequestRole = "list"
	RequestRoleDetail RequestRole = "detail"
	RequestRoleBody   RequestRole = "body"
)

type requestRoleKey struct{}

func WithRequestRole(ctx context.Context, role RequestRole) context.Context {
	switch role {
	case RequestRoleList, RequestRoleDetail, RequestRoleBody:
	default:
		role = RequestRoleOther
	}
	return context.WithValue(ctx, requestRoleKey{}, role)
}
func roleFrom(ctx context.Context) RequestRole {
	if role, ok := ctx.Value(requestRoleKey{}).(RequestRole); ok {
		return role
	}
	return RequestRoleOther
}

type RequestTotals struct {
	Requests       int64   `json:"requests"`
	Failures       int64   `json:"http_failures"`
	List           int64   `json:"list_requests"`
	Detail         int64   `json:"detail_requests"`
	Body           int64   `json:"body_requests"`
	Other          int64   `json:"other_requests"`
	HeaderSeconds  float64 `json:"http_header_seconds_sum"`
	LimiterSeconds float64 `json:"limiter_wait_seconds_sum"`
}
type PollRequests struct {
	mu     sync.Mutex
	totals RequestTotals
}
type pollRequestsKey struct{}

func WithPollRequests(ctx context.Context) (context.Context, *PollRequests) {
	p := &PollRequests{}
	return context.WithValue(ctx, pollRequestsKey{}, p), p
}
func (p *PollRequests) Snapshot() RequestTotals { p.mu.Lock(); defer p.mu.Unlock(); return p.totals }
func observePollRequest(ctx context.Context, duration, wait time.Duration, failed bool) {
	p, ok := ctx.Value(pollRequestsKey{}).(*PollRequests)
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totals.Requests++
	if failed {
		p.totals.Failures++
	}
	p.totals.HeaderSeconds += duration.Seconds()
	p.totals.LimiterSeconds += wait.Seconds()
	switch roleFrom(ctx) {
	case RequestRoleList:
		p.totals.List++
	case RequestRoleDetail:
		p.totals.Detail++
	case RequestRoleBody:
		p.totals.Body++
	default:
		p.totals.Other++
	}
}
