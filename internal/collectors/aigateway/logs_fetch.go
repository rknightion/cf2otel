package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

type capturedLog struct {
	detail      logRow
	bodies      [2]json.RawMessage
	unavailable [2]bool
}

// The detail carries each body as a head with a completeness flag. A complete
// head is the whole body, so only an incomplete side costs a raw read; any
// remaining reads overlap. All parsing, buffering and emission stays on the caller.
func (c *logs) fetchLog(ctx context.Context, path string) (capturedLog, error) {
	var result capturedLog
	var raw cfapi.RawGetter
	if c.cfg.AIGateway.CaptureBodies {
		var ok bool
		raw, ok = c.api.(cfapi.RawGetter)
		if !ok {
			return result, errors.New("aigateway API does not support raw body reads")
		}
	}
	if err := c.api.Get(ctx, path, nil, &result.detail); err != nil {
		return capturedLog{}, fmt.Errorf("aigateway log detail: %w", err)
	}
	if raw == nil {
		return result, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fatal := make(chan error, 1)
	var wg sync.WaitGroup
	heads := [2]struct {
		head     json.RawMessage
		complete *bool
	}{{result.detail.RequestHead, result.detail.RequestHeadComplete}, {result.detail.ResponseHead, result.detail.ResponseHeadComplete}}
	for i, side := range []string{"request", "response"} {
		if body, ok := completeHead(heads[i].head, heads[i].complete); ok {
			result.bodies[i] = body
			continue
		}
		wg.Go(func() {
			if err := raw.GetRaw(ctx, path+"/"+side, nil, &result.bodies[i]); err != nil {
				var httpErr *cfapi.HTTPError
				if errors.As(err, &httpErr) && httpErr.Status == 404 && httpErr.Code == 7002 {
					result.unavailable[i] = true
					return
				}
				select {
				case fatal <- fmt.Errorf("aigateway %s body: %w", side, err):
				default:
				}
				cancel()
			}
		})
	}
	wg.Wait()
	select {
	case err := <-fatal:
		return capturedLog{}, err
	default:
		return result, nil
	}
}

// completeHead returns the body a head holds when the gateway marks it
// complete. Live heads are JSON strings of the body text; any other shape, an
// empty head or a missing flag leaves the side to its raw read.
func completeHead(head json.RawMessage, complete *bool) (json.RawMessage, bool) {
	if complete == nil || !*complete {
		return nil, false
	}
	var body string
	if json.Unmarshal(head, &body) != nil || body == "" {
		return nil, false
	}
	return json.RawMessage(body), true
}

// detailConcurrency bounds log details in flight. A detail is latency-bound
// (up to ~4 MB of heads), so one at a time leaves most of the quota idle.
const detailConcurrency = 3

func (c *logs) detailPath(gateway, id string) string {
	return "/accounts/" + url.PathEscape(c.cfg.Cloudflare.AccountID) + "/ai-gateway/gateways/" + url.PathEscape(gateway) + "/logs/" + url.PathEscape(id)
}

type fetchedLog struct {
	log capturedLog
	err error
}

// detailPrefetch fetches row details at most detailConcurrency ahead of the
// consumer, which reads them strictly in order. A nil prefetch means rows need
// no detail and emit fetches nothing.
type detailPrefetch struct {
	c       *logs
	ctx     context.Context
	cancel  context.CancelFunc
	rows    []namedRow
	results []chan fetchedLog
	wg      sync.WaitGroup
}

func (c *logs) prefetchDetails(ctx context.Context, rows []namedRow) *detailPrefetch {
	if !c.cfg.AIGateway.CaptureBodies && !c.cfg.AIGateway.LinkCallerTraces {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &detailPrefetch{c: c, ctx: ctx, cancel: cancel, rows: rows, results: make([]chan fetchedLog, len(rows))}
	for i := 0; i < min(detailConcurrency, len(rows)); i++ {
		p.start(i)
	}
	return p
}

func (p *detailPrefetch) start(i int) {
	p.results[i] = make(chan fetchedLog, 1)
	p.wg.Go(func() {
		log, err := p.c.fetchLog(p.ctx, p.c.detailPath(p.rows[i].gateway, p.rows[i].row.ID))
		p.results[i] <- fetchedLog{log, err}
	})
}

// get returns row i's detail and starts the next one, keeping the window full.
func (p *detailPrefetch) get(i int) *fetchedLog {
	if p == nil {
		return nil
	}
	result := <-p.results[i]
	if next := i + detailConcurrency; next < len(p.rows) {
		p.start(next)
	}
	return &result
}

// stop cancels unread fetches and waits for them, so none outlives its window.
func (p *detailPrefetch) stop() {
	if p == nil {
		return
	}
	p.cancel()
	p.wg.Wait()
}
