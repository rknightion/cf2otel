package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	env "github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/rknightion/cf2otel/internal/semconv"
)

const EnvPrefix = "CF2OTEL_"

// Secret never exposes its value in text or JSON dumps.
type Secret string

func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return "[REDACTED]"
}
func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }
func (s Secret) Value() string                { return string(s) }

type ZonesConfig struct {
	Exclude []string `yaml:"exclude" json:"exclude"`
}

type PrometheusConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Listen  string `yaml:"listen" json:"listen"`
}

type Config struct {
	DEX        DEXConfig                  `yaml:"dex" json:"dex"`
	WARP       WARPConfig                 `yaml:"warp" json:"warp"`
	Prometheus PrometheusConfig           `yaml:"prometheus" json:"prometheus"`
	Firewall   FirewallConfig             `yaml:"firewall" json:"firewall"`
	Cloudflare CloudflareConfig           `yaml:"cloudflare" json:"cloudflare"`
	Collectors map[string]CollectorConfig `yaml:"collectors" json:"collectors"`
	Access     AccessConfig               `yaml:"access" json:"access"`
	HTTP       HTTPConfig                 `yaml:"http" json:"http"`
	Platform   PlatformConfig             `yaml:"platform" json:"platform"`
	Identity   IdentityConfig             `yaml:"identity" json:"identity"`
	AIGateway  AIGatewayConfig            `yaml:"ai_gateway" json:"ai_gateway"`
	OTLP       OTLPConfig                 `yaml:"otlp" json:"otlp"`
	State      StateConfig                `yaml:"state" json:"state"`
	Health     HealthConfig               `yaml:"health" json:"health"`
	Log        LogConfig                  `yaml:"log" json:"log"`
	Zones      ZonesConfig                `yaml:"zones" json:"zones"`
}
type CloudflareConfig struct {
	APIToken         Secret        `yaml:"api_token" json:"api_token"`
	AccountID        string        `yaml:"account_id" json:"account_id"`
	Zones            []string      `yaml:"zones" json:"zones"`
	APIBase          string        `yaml:"api_base" json:"api_base"`
	Timeout          time.Duration `yaml:"timeout" json:"timeout"`
	MaxResponseBytes int64         `yaml:"max_response_bytes" json:"max_response_bytes"`
}
type CollectorConfig struct {
	Enabled         bool          `yaml:"enabled" json:"enabled"`
	Interval        time.Duration `yaml:"interval" json:"interval"`
	InitialLookback time.Duration `yaml:"initial_lookback" json:"initial_lookback"`
	MaxWindow       time.Duration `yaml:"max_window" json:"max_window"`
}
type AccessConfig struct {
	IncludeServiceTokens bool `yaml:"include_service_tokens" json:"include_service_tokens"`
}
type HTTPConfig struct {
	RequestSource            string   `yaml:"request_source" json:"request_source"`
	Breakdowns               []string `yaml:"breakdowns" json:"breakdowns"`
	Scope                    string   `yaml:"scope" json:"scope"`
	MetricsScope             string   `yaml:"metrics_scope" json:"metrics_scope"`
	Hosts                    []string `yaml:"hosts" json:"hosts"`
	Zones                    []string `yaml:"zones" json:"zones"`
	MaxMetricHostsPerZone    int      `yaml:"max_metric_hosts_per_zone" json:"max_metric_hosts_per_zone"`
	MaxMetricSeriesPerWindow int      `yaml:"max_metric_series_per_window" json:"max_metric_series_per_window"`

	HighCardinalityLimit int               `yaml:"high_cardinality_limit" json:"high_cardinality_limit"`
	HighCardinalityHosts []string          `yaml:"high_cardinality_hosts" json:"high_cardinality_hosts"`
	ErrorPathRoutes      map[string]string `yaml:"error_path_routes" json:"error_path_routes"`
}
type FirewallConfig struct {
	RuleDimensions           bool `yaml:"rule_dimensions" json:"rule_dimensions"`
	MaxMetricSeriesPerWindow int  `yaml:"max_metric_series_per_window" json:"max_metric_series_per_window"`
}

type DEXConfig struct {
	ResultWindow    time.Duration `yaml:"result_window" json:"result_window"`
	MaxTests        int           `yaml:"max_tests" json:"max_tests"`
	MaxMetricSeries int           `yaml:"max_metric_series" json:"max_metric_series"`
}

type WARPConfig struct {
	LastSeenWindow  time.Duration `yaml:"last_seen_window" json:"last_seen_window"`
	MaxMetricSeries int           `yaml:"max_metric_series" json:"max_metric_series"`
}

type PlatformConfig struct {
	MaxMetricSeriesPerWindow int `yaml:"max_metric_series_per_window" json:"max_metric_series_per_window"`
}
type IdentityConfig struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	MatchWindow   time.Duration `yaml:"match_window" json:"match_window"`
	MaxCandidates int           `yaml:"max_candidates" json:"max_candidates"`
}
type AIGatewayConfig struct {
	Gateways         []string `yaml:"gateways" json:"gateways"`
	CaptureBodies    bool     `yaml:"capture_bodies" json:"capture_bodies"`
	MaxBodyBytes     int64    `yaml:"max_body_bytes" json:"max_body_bytes"`
	LinkCallerTraces bool     `yaml:"link_caller_traces" json:"link_caller_traces"`
}
type OTLPConfig struct {
	MetricDenylist         []string           `yaml:"metric_denylist" json:"metric_denylist"`
	AttributeDenylist      []string           `yaml:"attribute_denylist" json:"attribute_denylist"`
	MetricCardinalityLimit int                `yaml:"metric_cardinality_limit" json:"metric_cardinality_limit"`
	Endpoint               string             `yaml:"endpoint" json:"endpoint"`
	Protocol               string             `yaml:"protocol" json:"protocol"`
	GrafanaCloud           GrafanaCloudConfig `yaml:"grafana_cloud" json:"grafana_cloud"`
	Headers                map[string]string  `yaml:"headers" json:"headers"`
}

func (o OTLPConfig) MarshalJSON() ([]byte, error) {
	type safe OTLPConfig
	copy := safe(o)
	copy.Headers = make(map[string]string, len(o.Headers))
	for key := range o.Headers {
		copy.Headers[key] = "[REDACTED]"
	}
	return json.Marshal(copy)
}

type GrafanaCloudConfig struct {
	InstanceID string `yaml:"instance_id" json:"instance_id"`
	Token      Secret `yaml:"token" json:"token"`
}
type StateConfig struct {
	Dir string `yaml:"dir" json:"dir"`
}
type HealthConfig struct {
	Listen string `yaml:"listen" json:"listen"`
}
type LogConfig struct {
	Level  string `yaml:"level" json:"level"`
	Format string `yaml:"format" json:"format"`
}

var collectorNames = []string{
	"access.logins", "access.login_metrics", "access.scim", "access.seats", "inventory.access",
	"httpreq.events", "httpreq.metrics", "aigateway.logs", "aigateway.metrics",
	"audit.logs", "firewall.events", "firewall.metrics", "dns.events", "dns.metrics",
	"rum.pageloads", "rum.web_vitals", "gateway.dns",
	"workers.overview", "workers.invocations", "turnstile.events", "logpush.health",
	"d1.analytics", "d1.queries", "d1.storage", "kv.operations", "kv.storage",
	"r2.bandwidth", "r2.catalog_data", "r2.catalog_maintenance", "r2.operations", "r2.storage", "r2.sql",
	"durableobjects.invocations", "durableobjects.periodic", "durableobjects.sql_storage", "durableobjects.subrequests",
	"queues.backlog", "queues.consumer", "queues.delayed_backlog", "queues.message_operations",
	"email.routing", "email.sending", "selfobs",
}

var disabledCollectorNames = []string{"aigateway.coverage", "logpush.failures", "healthchecks.events", "workersai.metrics"}

func Default() Config {
	c := Config{Cloudflare: CloudflareConfig{APIBase: "https://api.cloudflare.com/client/v4", Timeout: 30 * time.Second, MaxResponseBytes: 16 << 20}, Collectors: map[string]CollectorConfig{}, HTTP: HTTPConfig{RequestSource: "eyeball", Breakdowns: []string{"status", "origin_status", "country", "protocol", "tls_protocol", "method", "content_type"}, Scope: "access_protected", MaxMetricHostsPerZone: 1000, MaxMetricSeriesPerWindow: 10000}, Platform: PlatformConfig{MaxMetricSeriesPerWindow: 500}, Identity: IdentityConfig{Enabled: true, MatchWindow: 15 * time.Minute, MaxCandidates: 100000}, AIGateway: AIGatewayConfig{MaxBodyBytes: 16 << 10, LinkCallerTraces: true}, OTLP: OTLPConfig{MetricCardinalityLimit: 10000, Protocol: "http", Headers: map[string]string{}}, State: StateConfig{Dir: "/var/lib/cf2otel"}, Health: HealthConfig{Listen: "127.0.0.1:9464"}, Log: LogConfig{Level: "info", Format: "json"}}
	c.HTTP.HighCardinalityLimit = 500
	c.HTTP.HighCardinalityHosts = []string{}
	c.DEX = DEXConfig{ResultWindow: time.Hour, MaxTests: 1000, MaxMetricSeries: 500}
	c.Collectors[semconv.CollectorNameDEXTests] = CollectorConfig{Interval: 5 * time.Minute}
	c.Collectors[semconv.CollectorNameLBHealth] = CollectorConfig{Interval: 5 * time.Minute}
	c.WARP = WARPConfig{LastSeenWindow: 15 * time.Minute, MaxMetricSeries: 500}
	c.OTLP.MetricDenylist = []string{}
	c.OTLP.AttributeDenylist = []string{}
	c.Prometheus = PrometheusConfig{Listen: "127.0.0.1:9465"}
	c.Firewall = FirewallConfig{MaxMetricSeriesPerWindow: 500}
	for _, name := range collectorNames {
		c.Collectors[name] = CollectorConfig{Enabled: true, Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour}
	}
	for _, name := range disabledCollectorNames {
		c.Collectors[name] = CollectorConfig{Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour}
	}
	// Snapshot collectors have no lookback window or checkpoint key.
	c.Collectors["access.seats"] = CollectorConfig{Enabled: true, Interval: 15 * time.Minute}
	c.Collectors["certs.packs"] = CollectorConfig{Interval: time.Hour}
	c.Collectors["tunnels.status"] = CollectorConfig{Interval: time.Minute}
	c.Collectors["warp.fleet"] = CollectorConfig{Interval: 5 * time.Minute}
	c.Collectors["httpreq.transfer"] = CollectorConfig{Enabled: true, Interval: time.Hour}
	// Threat rollups use complete UTC hours; alignment and holdback belong to the collector.
	c.Collectors["httpreq.threats"] = CollectorConfig{Enabled: true, Interval: time.Hour, InitialLookback: time.Hour, MaxWindow: time.Hour}
	// GraphQL AI Gateway Groups showed unbounded ingestion lag in the verified
	// account. REST logs provide the wave-1 metrics; do not schedule Groups.
	metrics := c.Collectors["aigateway.metrics"]
	metrics.Enabled = false
	c.Collectors["aigateway.metrics"] = metrics
	audit := c.Collectors["audit.logs"]
	audit.InitialLookback = 24 * time.Hour
	c.Collectors["audit.logs"] = audit
	return c
}
func (c Config) Collector(name string) CollectorConfig { return c.Collectors[name] }

var secretKeys = []string{"cloudflare.api_token", "otlp.grafana_cloud.token"}

func Load(path string) (*Config, error) {
	k := koanf.New(".")
	if err := k.Load(structs.Provider(Default(), "yaml"), nil); err != nil {
		return nil, fmt.Errorf("defaults: %w", err)
	}
	if path != "" {
		only := koanf.New(".")
		if err := only.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("config YAML: %w", err)
		}
		for _, key := range secretKeys {
			if only.Exists(key) {
				return nil, fmt.Errorf("%s is secret and must be supplied through environment", key)
			}
		}
		for _, key := range only.Keys() {
			if strings.HasPrefix(key, "otlp.headers.") {
				return nil, fmt.Errorf("%s is secret and must be supplied through environment", key)
			}
		}
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("config YAML: %w", err)
		}
	}
	if err := k.Load(env.Provider(".", env.Opt{Prefix: EnvPrefix, TransformFunc: func(key, value string) (string, any) {
		if strings.HasPrefix(key, EnvPrefix+"COLLECTORS__") {
			return "", nil
		}
		name := strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(key, EnvPrefix)), "__", ".")
		if name == "http.breakdowns" || name == "http.high_cardinality_hosts" || name == "zones.exclude" || name == "otlp.metric_denylist" || name == "otlp.attribute_denylist" {
			if value == "" {
				return name, []string{}
			}
			return name, strings.Split(value, ",")
		}
		return name, value
	}}), nil); err != nil {
		return nil, fmt.Errorf("environment: %w", err)
	}
	var c Config
	if err := k.UnmarshalWithConf("", &c, koanf.UnmarshalConf{Tag: "yaml", DecoderConfig: &mapstructure.DecoderConfig{Result: &c, WeaklyTypedInput: true, ErrorUnused: true, DecodeHook: mapstructure.StringToTimeDurationHookFunc()}}); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := applyCollectorEnvironment(&c); err != nil {
		return nil, fmt.Errorf("environment: %w", err)
	}
	if err := c.OTLP.normalizeDenylists(); err != nil {
		return nil, err
	}
	return &c, nil
}

func applyCollectorEnvironment(c *Config) error {
	names := make(map[string][]string, len(Default().Collectors))
	for name := range Default().Collectors {
		form := strings.ToUpper(strings.ReplaceAll(name, ".", "_"))
		names[form] = append(names[form], name)
	}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, EnvPrefix+"COLLECTORS__") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(key, EnvPrefix+"COLLECTORS__"), "__")
		if len(parts) != 2 || len(names[parts[0]]) != 1 {
			return fmt.Errorf("%s has unknown or ambiguous collector name", key)
		}
		name := names[parts[0]][0]
		cfg := c.Collectors[name]
		switch parts[1] {
		case "ENABLED":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			cfg.Enabled = parsed
		case "INTERVAL", "INITIAL_LOOKBACK", "MAX_WINDOW":
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			switch parts[1] {
			case "INTERVAL":
				cfg.Interval = parsed
			case "INITIAL_LOOKBACK":
				cfg.InitialLookback = parsed
			case "MAX_WINDOW":
				cfg.MaxWindow = parsed
			}
		default:
			return fmt.Errorf("%s has unknown collector setting", key)
		}
		c.Collectors[name] = cfg
	}
	return nil
}
func (o *OTLPConfig) normalizeDenylists() error {
	for _, list := range []struct {
		key    string
		values *[]string
		known  func(string) bool
	}{
		{"otlp.metric_denylist", &o.MetricDenylist, func(name string) bool { _, ok := semconv.Metric(name); return ok }},
		{"otlp.attribute_denylist", &o.AttributeDenylist, semconv.IsAttribute},
	} {
		seen := make(map[string]struct{}, len(*list.values))
		out := make([]string, 0, len(*list.values))
		for _, name := range *list.values {
			if !list.known(name) {
				return fmt.Errorf("%s contains unknown semantic name %q", list.key, name)
			}
			if _, exists := seen[name]; !exists {
				out = append(out, name)
				seen[name] = struct{}{}
			}
		}
		*list.values = out
	}
	return nil
}

func (c Config) Validate() error {
	var issues []string
	if err := c.OTLP.normalizeDenylists(); err != nil {
		issues = append(issues, err.Error())
	}
	add := func(ok bool, msg string) {
		if !ok {
			issues = append(issues, msg)
		}
	}
	add(c.Cloudflare.APIToken != "", "cloudflare.api_token is required")
	add(c.Cloudflare.AccountID != "", "cloudflare.account_id is required")
	add(c.Cloudflare.Timeout > 0, "cloudflare.timeout must be positive")
	add(c.Cloudflare.MaxResponseBytes > 0, "cloudflare.max_response_bytes must be positive")
	add(c.OTLP.Endpoint != "", "otlp.endpoint is required")
	add(c.OTLP.Protocol == "http" || c.OTLP.Protocol == "grpc", "otlp.protocol must be http or grpc")
	add(c.OTLP.GrafanaCloud.InstanceID != "", "otlp.grafana_cloud.instance_id is required")
	add(c.OTLP.GrafanaCloud.Token != "", "otlp.grafana_cloud.token is required")
	add(c.OTLP.MetricCardinalityLimit >= 0, "otlp.metric_cardinality_limit must be nonnegative")
	add(c.HTTP.RequestSource == "eyeball" || c.HTTP.RequestSource == "all", "http.request_source must be eyeball or all")
	for _, breakdown := range c.HTTP.Breakdowns {
		switch breakdown {
		case "status", "origin_status", "country", "protocol", "tls_protocol", "method", "content_type", "colo", "asn", "error_path":
		default:
			issues = append(issues, "http.breakdowns contains invalid value: "+breakdown)
		}
	}
	add(c.HTTP.HighCardinalityLimit >= 1 && c.HTTP.HighCardinalityLimit <= 5000, "http.high_cardinality_limit must be between 1 and 5000")
	add(len(c.HTTP.HighCardinalityHosts) <= 50, "http.high_cardinality_hosts allows at most 50 hosts")
	for _, host := range c.HTTP.HighCardinalityHosts {
		add(validHighCardinalityHost(host), "http.high_cardinality_hosts requires lowercase bare ASCII DNS names")
	}
	add(len(c.HTTP.ErrorPathRoutes) <= 5000, "http.error_path_routes allows at most 5000 routes")
	templates := map[string]bool{}
	for name, template := range c.HTTP.ErrorPathRoutes {
		add(httpRouteName.MatchString(name) && name != "other" && name != "unknown" && !httpHexSegment.MatchString(name), "http.error_path_routes has an invalid safe route name")
		add(validHTTPRouteTemplate(template), "http.error_path_routes requires canonical normalized absolute path templates")
		add(!templates[template], "http.error_path_routes contains duplicate templates")
		templates[template] = true
	}
	for _, feature := range c.HTTP.Breakdowns {
		add(feature != "error_path" || len(c.HTTP.ErrorPathRoutes) > 0, "http.error_path_routes is required for error_path")
	}
	add(c.HTTP.Scope == "access_protected" || c.HTTP.Scope == "hosts" || c.HTTP.Scope == "all", "http.scope is invalid")
	add(c.HTTP.MetricsScope == "" || c.HTTP.MetricsScope == "access_protected" || c.HTTP.MetricsScope == "hosts" || c.HTTP.MetricsScope == "all", "http.metrics_scope is invalid")
	add(c.HTTP.Scope != "hosts" || len(c.HTTP.Hosts) > 0, "http.hosts is required for hosts scope")
	add(c.HTTP.MetricsScope != "hosts" || len(c.HTTP.Hosts) > 0, "http.hosts is required for metrics hosts scope")
	add(c.HTTP.MaxMetricHostsPerZone > 0, "http.max_metric_hosts_per_zone must be positive")
	add(c.HTTP.MaxMetricSeriesPerWindow > 0, "http.max_metric_series_per_window must be positive")
	add(c.Firewall.MaxMetricSeriesPerWindow > 0, "firewall.max_metric_series_per_window must be positive")
	add(c.Platform.MaxMetricSeriesPerWindow > 0, "platform.max_metric_series_per_window must be positive")
	add(c.DEX.ResultWindow >= time.Hour && c.DEX.ResultWindow <= 168*time.Hour, "dex.result_window must be between 1h and 168h")
	add(c.DEX.MaxTests >= 1 && c.DEX.MaxTests <= 10000, "dex.max_tests must be between 1 and 10000")
	add(c.DEX.MaxMetricSeries >= 6 && c.DEX.MaxMetricSeries <= 5000, "dex.max_metric_series must be between 6 and 5000 (six remainder slots)")
	add(c.WARP.LastSeenWindow > 0 && c.WARP.LastSeenWindow <= time.Hour, "warp.last_seen_window must be positive and at most 60m")
	add(c.WARP.MaxMetricSeries >= 1 && c.WARP.MaxMetricSeries <= 5000, "warp.max_metric_series must be between 1 and 5000")
	add(c.Identity.MatchWindow > 0, "identity.match_window must be positive")
	add(c.Identity.MaxCandidates > 0, "identity.max_candidates must be positive")
	add(c.AIGateway.MaxBodyBytes > 0, "ai_gateway.max_body_bytes must be positive")
	_, port, listenErr := net.SplitHostPort(c.Prometheus.Listen)
	portNumber, portErr := strconv.Atoi(port)
	add(listenErr == nil && portErr == nil && strings.Trim(port, "0123456789") == "" && portNumber >= 1 && portNumber <= 65535 && !strings.ContainsAny(c.Prometheus.Listen, "/@"), "prometheus.listen must be a host:port with numeric port 1..65535")
	add(c.State.Dir != "", "state.dir is required")
	add(strings.HasPrefix(c.Health.Listen, "127.0.0.1:") || strings.HasPrefix(c.Health.Listen, "localhost:"), "health.listen must bind loopback")
	for name, v := range c.Collectors {
		if v.Enabled {
			add(v.Interval > 0, name+".interval must be positive")
			add(v.InitialLookback >= 0, name+".initial_lookback must be nonnegative")
			if name != "certs.packs" && name != "tunnels.status" && name != "httpreq.transfer" && name != "access.seats" && name != "warp.fleet" && name != semconv.CollectorNameDEXTests && name != semconv.CollectorNameLBHealth {
				if name == "workersai.metrics" {
					add(v.MaxWindow >= 5*time.Minute, name+".max_window must be at least 5m for complete source buckets")
				} else {
					add(v.MaxWindow > 0, name+".max_window must be positive")
				}
			}
		}
	}
	if len(issues) == 0 {
		return nil
	}
	sort.Strings(issues)
	return errors.New(strings.Join(issues, "; "))
}

var (
	httpRouteName      = regexp.MustCompile(`^[A-Za-z][A-Za-z_-]{0,63}$`)
	httpHexSegment     = regexp.MustCompile(`^[A-Fa-f0-9]{8,}$`)
	httpNumericSegment = regexp.MustCompile(`^[0-9]+$`)
	httpUUIDSegment    = regexp.MustCompile(`^[A-Fa-f0-9]{8}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{12}$`)
	httpDNSLabel       = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

func validHighCardinalityHost(host string) bool {
	if len(host) == 0 || len(host) > 253 || net.ParseIP(host) != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !httpDNSLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// Templates never leave the route lookup. Reject rather than normalize config,
// so two names cannot ambiguously refer to one source route.
func validHTTPRouteTemplate(template string) bool {
	if !strings.HasPrefix(template, "/") || strings.HasPrefix(template, "//") || len(template) > 4096 || !utf8.ValidString(template) || strings.ContainsAny(template, "%?#\\\\*[]{}()|^$+") {
		return false
	}
	for _, r := range template {
		if r <= ' ' || r == 127 {
			return false
		}
	}
	for _, part := range strings.Split(template[1:], "/") {
		if part == ":number" || part == ":uuid" || part == ":hex" {
			continue
		}
		if strings.Contains(part, ":") || part == "." || part == ".." || httpNumericSegment.MatchString(part) || httpUUIDSegment.MatchString(part) || httpHexSegment.MatchString(part) {
			return false
		}
	}
	return true
}

func (c Config) RedactedJSON() ([]byte, error) { return json.MarshalIndent(c, "", "  ") }
func (c Config) String() string                { b, _ := c.RedactedJSON(); return string(b) }
func (c Config) WriteRedacted(f *os.File) error {
	b, err := c.RedactedJSON()
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	return err
}
