package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

const logBatchItems = 64
const logBatchBytes = 400 * 1024
const logBatchInterval = 5 * time.Second

// boundedLogExporter batches synchronously. Its callers wait for a full batch
// to export, so queue pressure cannot silently discard a window's records.
type boundedLogExporter struct {
	sdklog.Exporter
	mu      sync.Mutex
	pending []sdklog.Record
	bytes   int
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func newBoundedLogExporter(exporter sdklog.Exporter) *boundedLogExporter {
	b := &boundedLogExporter{Exporter: exporter, stop: make(chan struct{}), done: make(chan struct{})}
	go b.run()
	return b
}

func (b *boundedLogExporter) run() {
	defer close(b.done)
	ticker := time.NewTicker(logBatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := b.flush(ctx); err != nil {
				otel.Handle(err)
			}
			cancel()
		}
	}
}

func estimatedLogBytes(r *sdklog.Record) int {
	n := len(r.Body().Emit()) + 256
	r.WalkAttributes(func(a attribute.KeyValue) bool {
		n += len(a.Key) + len(a.Value.Emit())
		return true
	})
	return n
}

func (b *boundedLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range records {
		size := estimatedLogBytes(&records[i])
		if len(b.pending) > 0 && (len(b.pending) >= logBatchItems || b.bytes+size > logBatchBytes) {
			if err := b.flushLocked(ctx); err != nil {
				return err
			}
		}
		b.pending = append(b.pending, records[i].Clone())
		b.bytes += size
		if len(b.pending) >= logBatchItems || b.bytes >= logBatchBytes {
			if err := b.flushLocked(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *boundedLogExporter) flushLocked(ctx context.Context) error {
	if len(b.pending) == 0 {
		return nil
	}
	batch := b.pending
	b.pending = nil
	b.bytes = 0
	return b.Exporter.Export(ctx, batch)
}

func (b *boundedLogExporter) flush(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flushLocked(ctx)
}

func (b *boundedLogExporter) ForceFlush(ctx context.Context) error {
	return errors.Join(b.flush(ctx), b.Exporter.ForceFlush(ctx))
}

func (b *boundedLogExporter) Shutdown(ctx context.Context) error {
	b.once.Do(func() { close(b.stop) })
	select {
	case <-b.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return errors.Join(b.flush(ctx), b.Exporter.Shutdown(ctx))
}
