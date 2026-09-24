package semconv

const (
	EventDNSQuery    = "cloudflare.dns.query"
	MetricDNSQueries = "cloudflare.dns.queries"

	AttrDNSZone           = "cloudflare.dns.zone"
	AttrDNSQueryName      = "cloudflare.dns.query.name"
	AttrDNSQueryType      = "cloudflare.dns.query.type"
	AttrDNSResponseCode   = "cloudflare.dns.response.code"
	AttrDNSResponseCached = "cloudflare.dns.response.cached"
	AttrDNSResponseStale  = "cloudflare.dns.response.stale"
	AttrDNSProtocol       = "cloudflare.dns.protocol"
	AttrDNSColo           = "cloudflare.dns.colo"
	AttrDNSSourceIP       = "cloudflare.dns.source.ip"
	AttrDNSDestinationIP  = "cloudflare.dns.destination.ip"
	AttrDNSUpstreamIP     = "cloudflare.dns.upstream.ip"
	AttrDNSIPVersion      = "cloudflare.dns.ip.version"
	AttrDNSSampleInterval = "cloudflare.dns.sample.interval"
	AttrDNSQuerySize      = "cloudflare.dns.query.size"
	AttrDNSResponseSize   = "cloudflare.dns.response.size"
)
