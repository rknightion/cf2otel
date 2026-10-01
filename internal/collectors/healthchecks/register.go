// Package healthchecks reserves the health-check analytics registration seam.
package healthchecks

import "github.com/rknightion/cf2otel/internal/collector"

// Register is intentionally a no-op until the independent health-check collector
// implementation lands. Declaring its config and signals does not collect data.
func Register(collector.Deps) {}
