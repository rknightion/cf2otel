package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const MinimumInterval = 10 * time.Second

type Options struct {
	Config                                               string
	Version, Healthcheck, Validate, PrintEffectiveConfig bool
	Once, DryRun, ResetState                             bool
	Explore                                              string
	Since, Before                                        time.Time
	Datasets                                             []string
}

func ClampInterval(d time.Duration) time.Duration {
	if d < MinimumInterval {
		return MinimumInterval
	}
	return d
}

func Parse(args []string) (Options, error) {
	var o Options
	fs := flag.NewFlagSet("cf2otel", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Config, "config", "", "config YAML path")
	fs.BoolVar(&o.Version, "version", false, "print version")
	fs.BoolVar(&o.Healthcheck, "healthcheck", false, "probe /healthz")
	fs.BoolVar(&o.Validate, "validate", false, "validate config")
	fs.BoolVar(&o.PrintEffectiveConfig, "print-effective-config", false, "print redacted config")
	fs.BoolVar(&o.Once, "once", false, "collect once")
	fs.BoolVar(&o.DryRun, "dry-run", false, "avoid export")
	fs.BoolVar(&o.ResetState, "reset-state", false, "clear state")
	fs.StringVar(&o.Explore, "explore", "", "print dataset and permission guidance")
	var since, before, datasets string
	fs.StringVar(&since, "since", "", "window start RFC3339")
	fs.StringVar(&before, "before", "", "window end RFC3339")
	fs.StringVar(&datasets, "datasets", "", "comma-separated collector names")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	var err error
	if since != "" {
		o.Since, err = time.Parse(time.RFC3339, since)
		if err != nil {
			return o, fmt.Errorf("-since: %w", err)
		}
	}
	if before != "" {
		o.Before, err = time.Parse(time.RFC3339, before)
		if err != nil {
			return o, fmt.Errorf("-before: %w", err)
		}
	}
	if o.Since.IsZero() != o.Before.IsZero() {
		return o, errors.New("-since and -before must be supplied together")
	}
	if !o.Since.IsZero() && !o.Before.IsZero() && !o.Since.Before(o.Before) {
		return o, errors.New("-since must precede -before")
	}
	if o.DryRun && o.ResetState {
		return o, errors.New("-dry-run cannot be combined with -reset-state")
	}
	if o.DryRun && !o.Once && (o.Since.IsZero() || o.Before.IsZero()) {
		return o, errors.New("-dry-run requires -once or both -since and -before")
	}
	if datasets != "" {
		for _, d := range strings.Split(datasets, ",") {
			d = strings.TrimSpace(d)
			if d == "" {
				return o, errors.New("-datasets contains empty name")
			}
			o.Datasets = append(o.Datasets, d)
		}
	}
	if len(o.Datasets) > 0 && !o.Once && o.Since.IsZero() && o.Before.IsZero() {
		return o, errors.New("-datasets requires -once or -since and -before")
	}
	modes := 0
	for _, active := range []bool{o.Version, o.Healthcheck, o.Validate, o.PrintEffectiveConfig, o.Explore != ""} {
		if active {
			modes++
		}
	}
	if modes > 1 || (modes > 0 && (o.Once || o.DryRun || o.ResetState || len(o.Datasets) > 0 || !o.Since.IsZero() || !o.Before.IsZero())) {
		return o, errors.New("standalone modes cannot be combined with collection options")
	}
	return o, nil
}

// ConfigPermissionAdvisory reports unsafe mode and ownership without blocking startup.
func ConfigPermissionAdvisory(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("config file permission check: %v", err)
	}
	var problems []string
	if info.Mode().Perm() != 0o600 {
		problems = append(problems, fmt.Sprintf("mode %04o, want 0600", info.Mode().Perm()))
	}
	if !ownedByRuntime(info) {
		problems = append(problems, "owner is not runtime UID")
	}
	if len(problems) == 0 {
		return ""
	}
	return "config file permission advisory: " + strings.Join(problems, "; ")
}

// MissingPermission identifies the Cloudflare permission likely required by an explore request.
func MissingPermission(status int, path string) string {
	if status != 403 {
		return ""
	}
	p := strings.ToLower(path)
	switch {
	case strings.Contains(p, "/ai-gateway/") && (strings.HasSuffix(p, "/request") || strings.HasSuffix(p, "/response")):
		return "403: missing AI Gateway Read permission (AI Gateway Metadata Read does not permit bodies)"
	case strings.Contains(p, "/ai-gateway/"):
		return "403: missing AI Gateway Metadata Read permission"
	case strings.Contains(p, "/access/"):
		return "403: missing Access Read permission"
	case strings.Contains(p, "graphql"):
		return "403: verify dataset entitlement and availableFields for this zone or account"
	default:
		return "403: verify the Cloudflare token permission group for this dataset"
	}
}

// ExploreSummary keeps explore output shape-only and never echoes a response body.
func ExploreSummary(dataset string, status int, path string) string {
	if guidance := MissingPermission(status, path); guidance != "" {
		return dataset + ": " + guidance
	}
	return fmt.Sprintf("%s: HTTP %d", dataset, status)
}
