package statuspage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/rknightion/cf2otel/internal/config"
)

const userAgent = "cf2otel (+https://github.com/rknightion/cf2otel)"

type source struct {
	config config.StatuspageConfig
	client *http.Client
}

func (s *source) fetch(ctx context.Context, path string) ([]byte, error) {
	if err := s.config.Validate(); err != nil {
		return nil, err
	}
	base, err := url.Parse(s.config.BaseURL)
	if err != nil {
		return nil, errors.New("invalid statuspage base URL")
	}
	// Paths are API-root-relative even if the configured origin has a path.
	base.Path = path
	base.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, errors.New("statuspage request construction failed")
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		// Do not echo arbitrary configured URLs or response bodies in diagnostics.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("statuspage HTTP request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("statuspage HTTP status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, s.config.MaxResponseBytes+1))
	if err != nil {
		return nil, errors.New("statuspage response read failed")
	}
	if int64(len(b)) > s.config.MaxResponseBytes {
		return nil, errors.New("statuspage response exceeds max_response_bytes")
	}
	return b, nil
}

// Required fields use pointers: JSON zero values must not disguise absent/null
// fields as a healthy status, a non-group component or an empty update list.
type componentRow struct {
	Name   *string `json:"name"`
	Status *string `json:"status"`
	Group  *bool   `json:"group"`
}
type incidentRow struct {
	ID      *string      `json:"id"`
	Name    *string      `json:"name"`
	Status  *string      `json:"status"`
	Impact  *string      `json:"impact"`
	Updates *[]updateRow `json:"incident_updates"`
}
type updateRow struct {
	ID        *string `json:"id"`
	Status    *string `json:"status"`
	Body      *string `json:"body"`
	UpdatedAt *string `json:"updated_at"`
}

func decodeComponents(b []byte) ([]componentRow, error) {
	var response struct {
		Components *[]componentRow `json:"components"`
	}
	if err := json.Unmarshal(b, &response); err != nil || response.Components == nil {
		return nil, errors.New("invalid statuspage components schema")
	}
	for _, r := range *response.Components {
		if r.Name == nil || *r.Name == "" || r.Status == nil || *r.Status == "" || r.Group == nil {
			return nil, errors.New("invalid statuspage component fields")
		}
	}
	return *response.Components, nil
}
func decodeIncidents(b []byte) ([]incidentRow, error) {
	var response struct {
		Incidents *[]incidentRow `json:"incidents"`
	}
	if err := json.Unmarshal(b, &response); err != nil || response.Incidents == nil {
		return nil, errors.New("invalid statuspage incidents schema")
	}
	for _, r := range *response.Incidents {
		if r.ID == nil || *r.ID == "" || r.Name == nil || r.Status == nil || *r.Status == "" || r.Impact == nil || r.Updates == nil {
			return nil, errors.New("invalid statuspage incident fields")
		}
		for _, u := range *r.Updates {
			if u.ID == nil || *u.ID == "" || u.Status == nil || *u.Status == "" || u.Body == nil || u.UpdatedAt == nil {
				return nil, errors.New("invalid statuspage update fields")
			}
		}
	}
	return *response.Incidents, nil
}
