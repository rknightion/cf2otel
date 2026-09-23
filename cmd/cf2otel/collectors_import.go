package main

import (
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/access"
	"github.com/rknightion/cf2otel/internal/collectors/aigateway"
	"github.com/rknightion/cf2otel/internal/collectors/audit"
	"github.com/rknightion/cf2otel/internal/collectors/firewall"
	"github.com/rknightion/cf2otel/internal/collectors/httpreq"
	"github.com/rknightion/cf2otel/internal/collectors/inventory"
	"github.com/rknightion/cf2otel/internal/collectors/selfobs"
)

// registerCollectors is the frozen domain order. Domain stubs are intentionally
// empty until their independent lanes land; the root wires the calls here.
func registerCollectors(deps collector.Deps) {
	access.Register(deps)
	inventory.Register(deps)
	httpreq.Register(deps)
	aigateway.Register(deps)
	audit.Register(deps)
	firewall.Register(deps)
	selfobs.Register(deps)
}
