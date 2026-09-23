package semconv

const (
	EventFirewallEvent   = "cloudflare.firewall.event"
	MetricFirewallEvents = "cloudflare.firewall.events"

	AttrFirewallZone                 = "cloudflare.firewall.zone"
	AttrFirewallAction               = "cloudflare.firewall.action"
	AttrFirewallSource               = "cloudflare.firewall.source"
	AttrFirewallKind                 = "cloudflare.firewall.kind"
	AttrFirewallClientIP             = "cloudflare.firewall.client.ip"
	AttrFirewallCountry              = "cloudflare.firewall.client.country"
	AttrFirewallASN                  = "cloudflare.firewall.client.asn"
	AttrFirewallASNDescription       = "cloudflare.firewall.client.asn_description"
	AttrFirewallHost                 = "cloudflare.firewall.host"
	AttrFirewallMethod               = "cloudflare.firewall.method"
	AttrFirewallPath                 = "cloudflare.firewall.path"
	AttrFirewallQuery                = "cloudflare.firewall.query"
	AttrFirewallProtocol             = "cloudflare.firewall.protocol"
	AttrFirewallEdgeResponseStatus   = "cloudflare.firewall.edge_status_code"
	AttrFirewallOriginResponseStatus = "cloudflare.firewall.origin_status_code"
	AttrFirewallAttackScoreClass     = "cloudflare.firewall.waf_attack_score_class"
	AttrFirewallRuleID               = "cloudflare.firewall.rule_id"
	AttrFirewallRulesetID            = "cloudflare.firewall.ruleset_id"
	AttrFirewallRayID                = "cloudflare.firewall.ray_id"
	AttrFirewallColo                 = "cloudflare.firewall.colo"
	AttrFirewallUserAgent            = "cloudflare.firewall.user_agent"
)
