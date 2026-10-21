package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
