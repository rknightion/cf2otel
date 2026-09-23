package cfapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Observer receives one event per HTTP attempt. Implementations must use bounded labels.
type Observer func(method, route string, status int, duration time.Duration, retry bool)

type HTTPClient struct {
	base     string
	token    string
	client   *http.Client
	cap      int64
	observer Observer
	mu       sync.Mutex
	settings map[string]cachedSettings
}
type cachedSettings struct {
	value   DatasetSettings
	expires time.Time
}

func New(cfg config.CloudflareConfig) *HTTPClient { return NewObserved(cfg, nil) }
func NewObserved(cfg config.CloudflareConfig, obs Observer) *HTTPClient {
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.cloudflare.com/client/v4"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = 16 << 20
	}
	return &HTTPClient{base: strings.TrimRight(cfg.APIBase, "/"), token: cfg.APIToken.Value(), client: &http.Client{Timeout: cfg.Timeout, Transport: observedTransport{next: http.DefaultTransport, observer: obs}}, cap: cfg.MaxResponseBytes, observer: obs, settings: make(map[string]cachedSettings)}
}
func (c *HTTPClient) do(ctx context.Context, method, path string, query url.Values, body []byte) ([]byte, error) {
	// Guard before URL processing or any I/O.
	if method != http.MethodGet && (method != http.MethodPost || path != "/graphql") {
		return nil, fmt.Errorf("cloudflare client refuses %s %s", method, path)
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, errors.New("invalid API path")
	}
	u, err := url.Parse(c.base + path)
	if err != nil {
		return nil, err
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	for attempt := 0; attempt < 5; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.client.Do(req)
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		retry := err == nil && (status == 429 || status == 502 || status == 503 || status == 504)
		if err != nil {
			return nil, err
		}
		limited := io.LimitReader(resp.Body, c.cap+1)
		raw, readErr := io.ReadAll(limited)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(raw)) > c.cap {
			return nil, fmt.Errorf("cloudflare response exceeds %d bytes", c.cap)
		}
		if retry && attempt < 4 {
			delay := time.Duration(1<<attempt) * 100 * time.Millisecond
			if h := resp.Header.Get("Retry-After"); h != "" {
				if s, e := strconv.Atoi(h); e == nil {
					delay = time.Duration(s) * time.Second
				} else if when, e := http.ParseTime(h); e == nil {
					delay = time.Until(when)
				}
			}
			if delay < 0 {
				delay = 0
			}
			if delay > 10*time.Second {
				delay = 10 * time.Second
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("cloudflare HTTP %d: %s", status, safeError(raw))
		}
		return raw, nil
	}
	return nil, errors.New("cloudflare retries exhausted")
}
func safeError(raw []byte) string {
	var v struct {
		Errors []struct {
			Code int `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &v) == nil && len(v.Errors) > 0 {
		return fmt.Sprintf("code %d", v.Errors[0].Code)
	}
	return "request failed"
}
func (c *HTTPClient) Get(ctx context.Context, path string, query url.Values, out any) error {
	raw, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	var env struct {
		Success *bool           `json:"success"`
		Errors  json.RawMessage `json:"errors"`
		Result  json.RawMessage `json:"result"`
	}
	if err = json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if env.Success != nil && !*env.Success {
		return fmt.Errorf("cloudflare API: %s", safeError(raw))
	}
	if len(env.Result) == 0 {
		return errors.New("cloudflare result missing")
	}
	return json.Unmarshal(env.Result, out)
}
func (c *HTTPClient) GetRaw(ctx context.Context, path string, query url.Values, out any) error {
	raw, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (c *HTTPClient) GetPage(ctx context.Context, path string, query url.Values, out any) error {
	raw, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	var env struct {
		Success *bool           `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if env.Success != nil && !*env.Success {
		return fmt.Errorf("cloudflare API: %s", safeError(raw))
	}
	if len(env.Result) == 0 {
		return errors.New("cloudflare result missing")
	}
	return json.Unmarshal(raw, out)
}
func (c *HTTPClient) Accounts(ctx context.Context) ([]Account, error) {
	var a []Account
	err := c.Pages(ctx, "/accounts", nil, 100, &a)
	return a, err
}
func (c *HTTPClient) Zones(ctx context.Context) ([]Zone, error) {
	var z []Zone
	err := c.Pages(ctx, "/zones", nil, 100, &z)
	return z, err
}
func (c *HTTPClient) Gateways(ctx context.Context, accountID string) ([]Gateway, error) {
	var g []Gateway
	err := c.Pages(ctx, "/accounts/"+url.PathEscape(accountID)+"/ai-gateway/gateways", nil, 100, &g)
	return g, err
}

// Pages follows page/per_page until a short or empty page; total_count is unreliable.
func (c *HTTPClient) Pages(ctx context.Context, path string, query url.Values, perPage int, out any) error {
	if perPage <= 0 {
		perPage = 100
	}
	rows := make([]json.RawMessage, 0)
	for page := 1; page <= 10000; page++ {
		q := cloneValues(query)
		q.Set("page", strconv.Itoa(page))
		q.Set("per_page", strconv.Itoa(perPage))
		raw, err := c.do(ctx, http.MethodGet, path, q, nil)
		if err != nil {
			return err
		}
		var env Page
		if err = json.Unmarshal(raw, &env); err != nil {
			return err
		}
		var chunk []json.RawMessage
		if err = json.Unmarshal(env.Result, &chunk); err != nil {
			return err
		}
		rows = append(rows, chunk...)
		actualPageSize := perPage
		if env.ResultInfo.PerPage > 0 {
			actualPageSize = env.ResultInfo.PerPage
		}
		if len(chunk) < actualPageSize {
			break
		}
		if page == 10000 {
			return errors.New("cloudflare pagination limit reached")
		}
	}
	b, _ := json.Marshal(rows)
	return json.Unmarshal(b, out)
}

// Cursors follows result_info.cursor and rejects repeated cursors.
func (c *HTTPClient) Cursors(ctx context.Context, path string, query url.Values, out any) error {
	rows := make([]json.RawMessage, 0)
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 10000; i++ {
		q := cloneValues(query)
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		raw, err := c.do(ctx, http.MethodGet, path, q, nil)
		if err != nil {
			return err
		}
		var env Page
		if err = json.Unmarshal(raw, &env); err != nil {
			return err
		}
		var chunk []json.RawMessage
		if err = json.Unmarshal(env.Result, &chunk); err != nil {
			return err
		}
		rows = append(rows, chunk...)
		cursor = env.ResultInfo.Cursor
		if cursor == "" {
			b, _ := json.Marshal(rows)
			return json.Unmarshal(b, out)
		}
		if seen[cursor] {
			return errors.New("cloudflare cursor repeated")
		}
		seen[cursor] = true
	}
	return errors.New("cloudflare cursor pagination limit reached")
}
func cloneValues(v url.Values) url.Values {
	q := url.Values{}
	for k, values := range v {
		q[k] = append([]string(nil), values...)
	}
	return q
}

// observedTransport records each attempt, including retries, at a bounded route.
type observedTransport struct {
	next     http.RoundTripper
	observer Observer
}

func (t observedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	started := time.Now()
	resp, err := t.next.RoundTrip(r)
	if t.observer != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		retry := status == 429 || status == 502 || status == 503 || status == 504
		t.observer(r.Method, routeName(r.URL.Path), status, time.Since(started), retry)
	}
	return resp, err
}
func routeName(path string) string {
	parts := strings.Split(path, "/")
	for i := 1; i < len(parts); i++ {
		switch parts[i-1] {
		case "accounts":
			if parts[i] != "" {
				parts[i] = "{account_id}"
			}
		case "zones":
			if parts[i] != "" {
				parts[i] = "{zone_id}"
			}
		case "gateways":
			if parts[i] != "" {
				parts[i] = "{gateway_id}"
			}
		case "logs":
			if parts[i] != "" {
				parts[i] = "{log_id}"
			}
		}
	}
	return strings.Join(parts, "/")
}
