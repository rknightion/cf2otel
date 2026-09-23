package semconv

const (
	MetricScrapeSuccess     = "cf2otel.scrape.success"
	MetricScrapeDuration    = "cf2otel.scrape.duration"
	MetricScrapeErrors      = "cf2otel.scrape.errors"
	MetricCheckpointAge     = "cf2otel.checkpoint.age"
	MetricAPIRequests       = "cf2otel.api.requests"
	MetricAPIRetries        = "cf2otel.api.retries"
	MetricIdentityMatched   = "cf2otel.identity.matched"
	MetricIdentityUnmatched = "cf2otel.identity.unmatched"
	MetricIdentityAmbiguous = "cf2otel.identity.ambiguous"
)
