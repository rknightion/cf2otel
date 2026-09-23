package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	env "github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
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

type Config struct {
	Cloudflare CloudflareConfig           `yaml:"cloudflare" json:"cloudflare"`
	Collectors map[string]CollectorConfig `yaml:"collectors" json:"collectors"`
	Access     AccessConfig               `yaml:"access" json:"access"`
	HTTP       HTTPConfig                 `yaml:"http" json:"http"`
	Identity   IdentityConfig             `yaml:"identity" json:"identity"`
	AIGateway  AIGatewayConfig            `yaml:"ai_gateway" json:"ai_gateway"`
	OTLP       OTLPConfig                 `yaml:"otlp" json:"otlp"`
	State      StateConfig                `yaml:"state" json:"state"`
	Health     HealthConfig               `yaml:"health" json:"health"`
	Log        LogConfig                  `yaml:"log" json:"log"`
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
	Scope string   `yaml:"scope" json:"scope"`
	Hosts []string `yaml:"hosts" json:"hosts"`
	Zones []string `yaml:"zones" json:"zones"`
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
	Endpoint     string             `yaml:"endpoint" json:"endpoint"`
	Protocol     string             `yaml:"protocol" json:"protocol"`
	GrafanaCloud GrafanaCloudConfig `yaml:"grafana_cloud" json:"grafana_cloud"`
	Headers      map[string]string  `yaml:"headers" json:"headers"`
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

var collectorNames = []string{"access.logins", "access.login_metrics", "access.scim", "inventory.access", "httpreq.events", "httpreq.metrics", "aigateway.logs", "aigateway.metrics", "selfobs"}

func Default() Config {
	c := Config{Cloudflare: CloudflareConfig{APIBase: "https://api.cloudflare.com/client/v4", Timeout: 30 * time.Second, MaxResponseBytes: 16 << 20}, Collectors: map[string]CollectorConfig{}, HTTP: HTTPConfig{Scope: "access_protected"}, Identity: IdentityConfig{Enabled: true, MatchWindow: 15 * time.Minute, MaxCandidates: 100000}, AIGateway: AIGatewayConfig{MaxBodyBytes: 16 << 10, LinkCallerTraces: true}, OTLP: OTLPConfig{Protocol: "http", Headers: map[string]string{}}, State: StateConfig{Dir: "/var/lib/cf2otel"}, Health: HealthConfig{Listen: "127.0.0.1:9464"}, Log: LogConfig{Level: "info", Format: "json"}}
	for _, name := range collectorNames {
		c.Collectors[name] = CollectorConfig{Enabled: true, Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour}
	}
	// GraphQL AI Gateway Groups showed unbounded ingestion lag in the verified
	// account. REST logs provide the wave-1 metrics; do not schedule Groups.
	metrics := c.Collectors["aigateway.metrics"]
	metrics.Enabled = false
	c.Collectors["aigateway.metrics"] = metrics
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
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(key, EnvPrefix)), "__", "."), value
	}}), nil); err != nil {
		return nil, fmt.Errorf("environment: %w", err)
	}
	var c Config
	if err := k.UnmarshalWithConf("", &c, koanf.UnmarshalConf{Tag: "yaml", DecoderConfig: &mapstructure.DecoderConfig{Result: &c, WeaklyTypedInput: true, ErrorUnused: true, DecodeHook: mapstructure.StringToTimeDurationHookFunc()}}); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return &c, nil
}
func (c Config) Validate() error {
	var issues []string
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
	add(c.HTTP.Scope == "access_protected" || c.HTTP.Scope == "hosts" || c.HTTP.Scope == "all", "http.scope is invalid")
	add(c.HTTP.Scope != "hosts" || len(c.HTTP.Hosts) > 0, "http.hosts is required for hosts scope")
	add(c.Identity.MatchWindow > 0, "identity.match_window must be positive")
	add(c.Identity.MaxCandidates > 0, "identity.max_candidates must be positive")
	add(c.AIGateway.MaxBodyBytes > 0, "ai_gateway.max_body_bytes must be positive")
	add(c.State.Dir != "", "state.dir is required")
	add(strings.HasPrefix(c.Health.Listen, "127.0.0.1:") || strings.HasPrefix(c.Health.Listen, "localhost:"), "health.listen must bind loopback")
	for name, v := range c.Collectors {
		if v.Enabled {
			add(v.Interval > 0, name+".interval must be positive")
			add(v.InitialLookback >= 0, name+".initial_lookback must be nonnegative")
			add(v.MaxWindow > 0, name+".max_window must be positive")
		}
	}
	if len(issues) == 0 {
		return nil
	}
	sort.Strings(issues)
	return errors.New(strings.Join(issues, "; "))
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
