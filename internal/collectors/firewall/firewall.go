package firewall

import (
	"context"
	"fmt"
	"strings"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
)

const (
	rawDataset          = "firewallEventsAdaptive"
	groupsDataset       = "firewallEventsAdaptiveGroups"
	byTimeGroupsDataset = "firewallEventsAdaptiveByTimeGroups"
	queryLimit          = 10000
)

type datasetSettingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

func zones(ctx context.Context, cfg *config.Config, api cfapi.Client) ([]cfapi.Zone, error) {
	all, err := api.Zones(ctx)
	if err != nil {
		return nil, fmt.Errorf("list firewall zones: %w", err)
	}
	wanted := cfg.Cloudflare.Zones
	if len(wanted) == 0 {
		return all, nil
	}
	selected := make([]cfapi.Zone, 0, len(wanted))
	matched := make([]bool, len(wanted))
	for _, zone := range all {
		included := false
		for i, nameOrID := range wanted {
			if nameOrID == zone.ID || strings.EqualFold(nameOrID, zone.Name) {
				matched[i] = true
				included = true
			}
		}
		if included {
			selected = append(selected, zone)
		}
	}
	for _, found := range matched {
		if !found {
			return nil, fmt.Errorf("configured firewall zone absent from discovery")
		}
	}
	return selected, nil
}

func datasetSettings(ctx context.Context, api cfapi.Client, zoneID, dataset string) (cfapi.DatasetSettings, error) {
	reader, ok := api.(datasetSettingsReader)
	if !ok {
		return cfapi.DatasetSettings{}, fmt.Errorf("cloudflare client does not expose dataset settings")
	}
	settings, err := reader.DatasetSettings(ctx, cfapi.ZoneScope, zoneID, dataset)
	if err != nil {
		return cfapi.DatasetSettings{}, fmt.Errorf("read %s settings: %w", dataset, err)
	}
	return settings, nil
}

func hasAvailableField(fields []string, wanted string) bool {
	for _, field := range fields {
		if strings.EqualFold(field, wanted) {
			return true
		}
		if prefix, suffix, ok := strings.Cut(wanted, "."); ok && strings.EqualFold(field, prefix+"_"+suffix) {
			return true
		}
	}
	return false
}
