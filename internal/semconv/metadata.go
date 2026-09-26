package semconv

// MetricSpec holds instrument metadata for a declared metric.
type MetricSpec struct {
	Unit        string
	Description string
	Boundaries  []float64
}

// The operation buckets resolve latency through a minute; poller buckets also
// resolve short Cloudflare API calls and collector scrapes.
var genAIOperationDurationBuckets = []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 30, 60}
var pollerDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

var metricSpecs = map[string]MetricSpec{
	MetricWindowGap:                      {Unit: "s", Description: "Skipped retention-gap seconds by collector."},
	MetricAccessLogins:                   {Unit: "1", Description: "Human Access login count from cf1AccessLoginsRawGroups."},
	MetricAccessIdentityLogins:           {Unit: "1", Description: "Exact REST identity-login count by app, allowed, connection and action; excludes nonidentity service-token rows."},
	MetricAccessRequests:                 {Unit: "{request}", Description: "Access request count from accessLoginRequestsAdaptiveGroups; keep nonidentity traffic separate."},
	MetricAccessApps:                     {Unit: "1", Description: "Access application inventory gauge."},
	MetricAccessUsers:                    {Unit: "1", Description: "Access user inventory gauge."},
	MetricHTTPRequests:                   {Unit: "{request}", Description: "Request count from sample-corrected httpRequestsAdaptiveGroups."},
	MetricHTTPOriginDuration:             {Unit: "s", Description: "Average origin response duration per Groups window, in seconds."},
	MetricAuditEvents:                    {Unit: "1", Description: "Exact audit event count by resource product, action type and action result."},
	MetricFirewallEvents:                 {Unit: "1", Description: "Security event count from a Groups dataset by zone. Pro Groups provides action and source dimensions; Free ByTimeGroups rejects them despite settings.availableFields advertising them."},
	MetricDNSQueries:                     {Unit: "1", Description: "DNS query count from dnsAnalyticsAdaptiveGroups by zone and available bounded dimensions."},
	MetricGatewayDNSQueries:              {Unit: "1", Description: "Gateway DNS query sum from account-level cf1GatewayDnsRawGroups, by bounded query type, resolver decision and country."},
	MetricRUMPageViews:                   {Unit: "1", Description: "Page views from rumPageloadEventsAdaptiveGroups by country and device."},
	MetricRUMSessions:                    {Unit: "1", Description: "Visit sum from rumPageloadEventsAdaptiveGroups by country and device."},
	MetricRUMLCPP75:                      {Unit: "s", Description: "Rolling p75 largest contentful paint in seconds, converted from GraphQL milliseconds."},
	MetricRUMINPP75:                      {Unit: "s", Description: "Rolling p75 interaction to next paint in seconds, converted from GraphQL milliseconds."},
	MetricRUMFIDP75:                      {Unit: "s", Description: "Rolling p75 first input delay in seconds, converted from GraphQL milliseconds."},
	MetricRUMFCPP75:                      {Unit: "s", Description: "Rolling p75 first contentful paint in seconds, converted from GraphQL milliseconds."},
	MetricRUMTTFBP75:                     {Unit: "s", Description: "Rolling p75 time to first byte in seconds, converted from GraphQL milliseconds."},
	MetricRUMCLSP75:                      {Unit: "1", Description: "Rolling p75 cumulative layout shift score gauge."},
	MetricWorkersRequests:                {Unit: "{request}", Description: "Worker request count from workersOverviewRequestsAdaptiveGroups, optionally by bounded script name."},
	MetricTurnstileEvents:                {Unit: "1", Description: "Turnstile event count from turnstileAdaptiveGroups, account aggregate."},
	MetricLogpushUploads:                 {Unit: "1", Description: "Logpush upload count from logpushHealthAdaptiveGroups, account aggregate."},
	MetricLogpushRecords:                 {Unit: "1", Description: "Logpush record count from logpushHealthAdaptiveGroups, account aggregate."},
	MetricD1ReadQueries:                  {Unit: "1", Description: "D1 read query sum from d1AnalyticsAdaptiveGroups, account aggregate."},
	MetricD1WriteQueries:                 {Unit: "1", Description: "D1 write query sum from d1AnalyticsAdaptiveGroups, account aggregate."},
	MetricD1Queries:                      {Unit: "1", Description: "D1 query count from d1QueriesAdaptiveGroups; query text is not selected."},
	MetricD1StorageBytes:                 {Unit: "By", Description: "Maximum D1 database size across databases in the latest complete bucket, By."},
	MetricKVRequests:                     {Unit: "{request}", Description: "KV operation request sum from kvOperationsAdaptiveGroups, account aggregate."},
	MetricKVStorageBytes:                 {Unit: "By", Description: "Maximum KV namespace bytes in the latest complete bucket, By."},
	MetricKVStorageKeys:                  {Unit: "{key}", Description: "Maximum KV namespace key count in the latest complete bucket."},
	MetricR2BandwidthDownloadBytes:       {Unit: "By", Description: "R2 download byte sum, optionally by bounded bucket name, By."},
	MetricR2BandwidthUploadBytes:         {Unit: "By", Description: "R2 upload byte sum, optionally by bounded bucket name, By."},
	MetricR2CatalogDataOperations:        {Unit: "1", Description: "R2 catalog data operation count, optionally by bounded namespace name."},
	MetricR2CatalogMaintenanceJobs:       {Unit: "1", Description: "R2 catalog maintenance job count, optionally by bounded namespace name."},
	MetricR2Requests:                     {Unit: "{request}", Description: "R2 operation request sum, optionally by bounded bucket name."},
	MetricR2StoragePayloadBytes:          {Unit: "By", Description: "R2 payload-size gauge in the latest complete bucket per bucket, By."},
	MetricR2StorageObjects:               {Unit: "{object}", Description: "R2 object-count gauge in the latest complete bucket per bucket."},
	MetricR2SQLQueries:                   {Unit: "1", Description: "R2 SQL query count, optionally by bounded bucket name; table names are omitted."},
	MetricDurableObjectsRequests:         {Unit: "{request}", Description: "Durable Objects invocation request sum, account aggregate."},
	MetricDurableObjectsSubrequests:      {Unit: "{request}", Description: "Durable Objects periodic subrequest sum, account aggregate."},
	MetricDurableObjectsSQLStorageBytes:  {Unit: "By", Description: "Maximum Durable Objects SQL storage across namespaces in the latest complete bucket, By."},
	MetricDurableObjectsRequestBodyBytes: {Unit: "By", Description: "Durable Objects uncached request-body byte sum, account aggregate, By."},
	MetricQueuesBacklogMessages:          {Unit: "{message}", Description: "Maximum per-queue average backlog messages in the latest complete bucket."},
	MetricQueuesBacklogBytes:             {Unit: "By", Description: "Maximum per-queue average backlog bytes in the latest complete bucket, By."},
	MetricQueuesConsumerConcurrency:      {Unit: "{consumer}", Description: "Maximum per-queue average consumer concurrency in the latest complete bucket."},
	MetricQueuesDelayedBacklogMessages:   {Unit: "{message}", Description: "Maximum per-queue average delayed backlog messages in the latest complete bucket."},
	MetricQueuesMessageOperations:        {Unit: "1", Description: "Queue message operation count, account aggregate."},
	MetricQueuesBillableOperations:       {Unit: "1", Description: "Queue billable operation sum, account aggregate."},
	MetricAIGatewayRequests:              {Unit: "{request}", Description: "AI Gateway request count."},
	MetricAIGatewayErrors:                {Unit: "1", Description: "AI Gateway error count."},
	MetricAIGatewayCacheHits:             {Unit: "1", Description: "AI Gateway cache hits."},
	MetricAIGatewayCost:                  {Unit: "1", Description: "AI Gateway request cost."},
	MetricAIGatewayDLPRequests:           {Unit: "{request}", Description: "AI Gateway DLP-flagged or -blocked request count by gateway, action and direction."},
	MetricGenAIDuration:                  {Unit: "s", Description: "GenAI operation duration.", Boundaries: genAIOperationDurationBuckets},
	MetricGenAIInputTokens:               {Unit: "{token}", Description: "Input token usage."},
	MetricGenAIOutputTokens:              {Unit: "{token}", Description: "Output token usage."},
	MetricGenAICacheReadTokens:           {Unit: "{token}", Description: "Cached input tokens."},
	MetricGenAIReasoningTokens:           {Unit: "{token}", Description: "Reasoning output tokens."},
	MetricGenAIInputOperationTokens:      {Unit: "{token}", Description: "Input tokens by operation."},
	MetricGenAIOutputOperationTokens:     {Unit: "{token}", Description: "Output tokens by operation."},
	MetricScrapeSuccess:                  {Unit: "1", Description: "Collector scrape success state."},
	MetricScrapeDuration:                 {Unit: "s", Description: "Collector scrape duration.", Boundaries: pollerDurationBuckets},
	MetricScrapeErrors:                   {Unit: "1", Description: "Collector scrape errors."},
	MetricScrapeLastSuccess:              {Unit: "s", Description: "Time of last successful collector scrape."},
	MetricExportSuccess:                  {Unit: "1", Description: "Successful OTLP exports."},
	MetricExportErrors:                   {Unit: "1", Description: "Failed OTLP exports."},
	MetricBuildInfo:                      {Unit: "1", Description: "Build identity."},
	MetricCheckpointAge:                  {Unit: "s", Description: "Age of the oldest collector checkpoint."},
	MetricAPIRequests:                    {Unit: "{request}", Description: "Cloudflare API requests."},
	MetricAPIDuration:                    {Unit: "s", Description: "Cloudflare API request duration.", Boundaries: pollerDurationBuckets},
	MetricAPIRetries:                     {Unit: "1", Description: "Cloudflare API retries."},
	MetricIdentityMatched:                {Unit: "1", Description: "HTTP events matched to one Access identity."},
	MetricIdentityUnmatched:              {Unit: "1", Description: "HTTP events without a match."},
	MetricIdentityAmbiguous:              {Unit: "1", Description: "HTTP events with more than one candidate."},
	MetricWindowCommitFailures:           {Unit: "1", Description: "Failed window commits by retry or dropped outcome."},
	MetricWindowCatchupWindows:           {Unit: "1", Description: "Additional bounded collector windows committed in one scheduler tick."},
	MetricEmailRoutingEvents:             {Unit: "1", Description: "Email Routing event count."},
	MetricEmailSendingEvents:             {Unit: "1", Description: "Email Sending event count."},
}

// Metric returns metadata for a metric declared by this package.
func Metric(name string) (MetricSpec, bool) {
	spec, ok := metricSpecs[name]
	return spec, ok
}
