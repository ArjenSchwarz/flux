package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// Config holds all configuration for the Flux poller, loaded from environment variables.
type Config struct {
	// AlphaESS credentials
	AppID     string
	AppSecret string
	Serial    string

	// Location is the timezone every calendar date and wall-clock boundary is
	// interpreted in. The off-peak window used to live here too; it is now a
	// property of the pricing plan that prices each day (Decision 2), so it
	// switches with the plan instead of needing a redeploy.
	Location *time.Location

	// DynamoDB table names (empty in dry-run mode)
	TableReadings     string
	TableDailyEnergy  string
	TableDailyPower   string
	TableSystem       string
	TableOffpeak      string
	TableDevices      string
	TableSocRules     string
	TableSocFireState string
	// TablePricing holds the band-based plans. The poller reads it (never
	// writes) to resolve each day's free window — the plan replaced the SSM
	// window parameters as the source of truth (Decision 2).
	TablePricing string

	// APNs SSM parameter paths (empty in dry-run mode and when SoC alerts
	// are not deployed). When all are set, the poller wires the alert path.
	// The APNs environment is carried per device on the registration row,
	// not loaded from SSM, so two users on different builds (Xcode dev /
	// TestFlight / App Store) coexist on the same poller.
	APNsKeyParam      string
	APNsKeyIDParam    string
	APNsTeamIDParam   string
	APNsBundleIDParam string

	// Runtime
	AWSRegion   string
	DryRun      bool
	HTTPTimeout time.Duration
}

// Load reads configuration from environment variables and validates it.
// All validation errors are collected and reported together.
func Load() (*Config, error) {
	var errs []error

	cfg := &Config{
		HTTPTimeout: 10 * time.Second,
	}

	// DRY_RUN check first — affects which vars are required.
	cfg.DryRun = os.Getenv("DRY_RUN") == "true"

	// Always-required vars.
	cfg.AppID = requireEnv("ALPHA_APP_ID", &errs)
	cfg.AppSecret = requireEnv("ALPHA_APP_SECRET", &errs)
	cfg.Serial = requireEnv("SYSTEM_SERIAL", &errs)

	// Timezone.
	tz := os.Getenv("TZ")
	if tz == "" {
		tz = "Australia/Sydney"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		errs = append(errs, fmt.Errorf("TZ: invalid timezone %q: %w", tz, err))
	} else {
		cfg.Location = loc
	}

	// AWS/DynamoDB vars — only required when not in dry-run mode.
	if !cfg.DryRun {
		cfg.AWSRegion = requireEnv("AWS_REGION", &errs)
		cfg.TableReadings = requireEnv("TABLE_READINGS", &errs)
		cfg.TableDailyEnergy = requireEnv("TABLE_DAILY_ENERGY", &errs)
		cfg.TableDailyPower = requireEnv("TABLE_DAILY_POWER", &errs)
		cfg.TableSystem = requireEnv("TABLE_SYSTEM", &errs)
		cfg.TableOffpeak = requireEnv("TABLE_OFFPEAK", &errs)
		cfg.TablePricing = requireEnv("TABLE_PRICING", &errs)
		// SoC alert tables and APNs SSM params are optional: when missing
		// the poller starts without the SoC alert path. Production wires
		// them via CloudFormation; integration tests leave them unset.
		cfg.TableDevices = os.Getenv("TABLE_DEVICES")
		cfg.TableSocRules = os.Getenv("TABLE_SOC_RULES")
		cfg.TableSocFireState = os.Getenv("TABLE_SOC_FIRESTATE")
		cfg.APNsKeyParam = os.Getenv("APNS_KEY_PARAM")
		cfg.APNsKeyIDParam = os.Getenv("APNS_KEY_ID_PARAM")
		cfg.APNsTeamIDParam = os.Getenv("APNS_TEAM_ID_PARAM")
		cfg.APNsBundleIDParam = os.Getenv("APNS_BUNDLE_ID_PARAM")
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	slog.Debug("config loaded",
		"serial", cfg.Serial,
		"tz", cfg.Location.String(),
		"dry_run", cfg.DryRun,
	)

	return cfg, nil
}

// requireEnv reads an environment variable and appends an error if it's missing or empty.
func requireEnv(name string, errs *[]error) string {
	v := os.Getenv(name)
	if v == "" {
		*errs = append(*errs, fmt.Errorf("required environment variable %s is missing or empty", name))
	}
	return v
}

// SocAlertsConfigured reports whether all SoC alert env vars are present.
// When false, the poller starts without the alert pipeline — useful for
// the gradual rollout described in design.md §Deploy ordering.
func (c *Config) SocAlertsConfigured() bool {
	return c.TableDevices != "" && c.TableSocRules != "" && c.TableSocFireState != "" &&
		c.APNsKeyParam != "" && c.APNsKeyIDParam != "" && c.APNsTeamIDParam != "" &&
		c.APNsBundleIDParam != ""
}
