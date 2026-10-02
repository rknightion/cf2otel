package kv

import (
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// Replaces the intentionally retired account-only label expectation without
// admitting IDs or arbitrary attributes.
func expectedRemainder(attrs []telemetry.Attr) bool {
	return len(attrs) == 1 && attrs[0].Key == semconv.AttrKVNamespaceName && attrs[0].Value == "other"
}
