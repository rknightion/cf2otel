package semconv

const (
	MetricHealthCheckEvents        = "cloudflare.health_check.events"
	MetricHealthCheckRTT           = "cloudflare.health_check.rtt"
	MetricHealthCheckTTFB          = "cloudflare.health_check.ttfb"
	MetricHealthCheckTCPConnection = "cloudflare.health_check.tcp_connection"
	MetricHealthCheckTLSHandshake  = "cloudflare.health_check.tls_handshake"
	AttrHealthCheckZone            = "cloudflare.health_check.zone"
	AttrHealthCheckStatus          = "cloudflare.health_check.status"
	AttrHealthCheckFailureReason   = "cloudflare.health_check.failure_reason"
	AttrHealthCheckOrigin          = "cloudflare.health_check.origin"
)
