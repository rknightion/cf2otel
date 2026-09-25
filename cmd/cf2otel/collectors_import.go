package main

import (
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/access"
	"github.com/rknightion/cf2otel/internal/collectors/aigateway"
	"github.com/rknightion/cf2otel/internal/collectors/audit"
	"github.com/rknightion/cf2otel/internal/collectors/d1"
	"github.com/rknightion/cf2otel/internal/collectors/dns"
	"github.com/rknightion/cf2otel/internal/collectors/durableobjects"
	"github.com/rknightion/cf2otel/internal/collectors/email"
	"github.com/rknightion/cf2otel/internal/collectors/firewall"
	"github.com/rknightion/cf2otel/internal/collectors/gateway"
	"github.com/rknightion/cf2otel/internal/collectors/httpreq"
	"github.com/rknightion/cf2otel/internal/collectors/inventory"
	"github.com/rknightion/cf2otel/internal/collectors/kv"
	"github.com/rknightion/cf2otel/internal/collectors/logpush"
	"github.com/rknightion/cf2otel/internal/collectors/queues"
	"github.com/rknightion/cf2otel/internal/collectors/r2"
	"github.com/rknightion/cf2otel/internal/collectors/rum"
	"github.com/rknightion/cf2otel/internal/collectors/selfobs"
	"github.com/rknightion/cf2otel/internal/collectors/turnstile"
	"github.com/rknightion/cf2otel/internal/collectors/workers"
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
	dns.Register(deps)
	rum.Register(deps)
	gateway.Register(deps)
	workers.Register(deps)
	turnstile.Register(deps)
	logpush.Register(deps)
	d1.Register(deps)
	kv.Register(deps)
	r2.Register(deps)
	durableobjects.Register(deps)
	queues.Register(deps)
	email.Register(deps)
	selfobs.Register(deps)
}
