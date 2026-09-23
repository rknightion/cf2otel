package inventory

import "github.com/rknightion/cf2otel/internal/collector"

// Register installs Access inventory collectors.
func Register(deps collector.Deps) { registerInventory(deps) }
