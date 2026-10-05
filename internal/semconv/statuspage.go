package semconv

const (
	CollectorNameStatuspageComponents = "statuspage.components"
	CollectorNameStatuspageIncidents  = "statuspage.incidents"
	MetricStatusComponentStatus       = "cloudflare.status.component.status"
	AttrStatusComponentName           = "cloudflare.status.component.name"
	AttrStatusComponentType           = "cloudflare.status.component.type"
	EventStatusIncidentUpdate         = "cloudflare.status.incident.update"
	AttrStatusIncidentID              = "cloudflare.status.incident.id"
	AttrStatusIncidentName            = "cloudflare.status.incident.name"
	AttrStatusIncidentStatus          = "cloudflare.status.incident.status"
	AttrStatusIncidentImpact          = "cloudflare.status.incident.impact"
	AttrStatusUpdateID                = "cloudflare.status.update.id"
	AttrStatusUpdateStatus            = "cloudflare.status.update.status"
	// AttrStatusUpdateTruncated is a string enum ("true"/"false"), not a boolean.
	AttrStatusUpdateTruncated = "cloudflare.status.update.truncated"
)
