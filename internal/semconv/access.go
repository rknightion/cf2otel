package semconv

const (
	EventAccessLogin             = "cloudflare.access.login"
	EventAccessSCIM              = "cloudflare.access.scim_update"
	MetricAccessLogins           = "cloudflare.access.logins"
	MetricAccessRequests         = "cloudflare.access.requests"
	MetricAccessApps             = "cloudflare.access.apps"
	MetricAccessUsers            = "cloudflare.access.users"
	AttrAccessUserEmail          = "cloudflare.access.user.email"
	AttrAccessUserID             = "cloudflare.access.user.id"
	AttrAccessIdentityInferred   = "cloudflare.access.identity.inferred"
	AttrAccessIdentityLoginRayID = "cloudflare.access.identity.login_ray_id"
	AttrAccessRayID              = "cloudflare.access.ray_id"
	AttrAccessApp                = "cloudflare.access.app"
	AttrAccessAppID              = "cloudflare.access.app.id"
	AttrAccessHost               = "cloudflare.access.host"
	AttrAccessPath               = "cloudflare.access.path"
	AttrAccessAction             = "cloudflare.access.action"
	AttrAccessAllowed            = "cloudflare.access.allowed"
	AttrAccessCountry            = "cloudflare.access.country"
	AttrAccessLoginType          = "cloudflare.access.login_type"
	AttrAccessIdentityProvider   = "cloudflare.access.identity_provider"
	AttrAccessServiceToken       = "cloudflare.access.service_token" //nolint:gosec // Attribute name, not a credential.
)
