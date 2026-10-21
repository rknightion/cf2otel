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

// Only independent network reads run concurrently. All parsing, buffering and
// emission stays on the caller, with at most three reads in flight per log.
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fatal := make(chan error, 1)
	var wg sync.WaitGroup
	read := func(fn func() error) {
		wg.Go(func() {
			if err := fn(); err != nil {
				select {
				case fatal <- err:
				default:
				}
				cancel()
			}
		})
	}
	read(func() error {
		if err := c.api.Get(ctx, path, nil, &result.detail); err != nil {
			return fmt.Errorf("aigateway log detail: %w", err)
		}
		return nil
	})
	if raw != nil {
		for i, side := range []string{"request", "response"} {
			read(func() error {
				if err := raw.GetRaw(ctx, path+"/"+side, nil, &result.bodies[i]); err != nil {
					var httpErr *cfapi.HTTPError
					if errors.As(err, &httpErr) && httpErr.Status == 404 && httpErr.Code == 7002 {
						result.unavailable[i] = true
						return nil
					}
					return fmt.Errorf("aigateway %s body: %w", side, err)
				}
				return nil
			})
		}
	}
	wg.Wait()
	select {
	case err := <-fatal:
		return capturedLog{}, err
	default:
		return result, nil
	}
}
