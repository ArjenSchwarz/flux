package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the Flux poller, loaded from environment variables.
type Config struct {
	// AlphaESS credentials
	AppID     string
	AppSecret string
	Serial    string

	// Off-peak window (duration from midnight)
	OffpeakStart time.Duration
	OffpeakEnd   time.Duration
	Location     *time.Location

	// DynamoDB table names (empty in dry-run mode)
	TableReadings     string
	TableDailyEnergy  string
	TableDailyPower   string
	TableSystem       string
	TableOffpeak      string
	TableDevices      string
	TableSocRules     string
	TableSocFireState string

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

	// Off-peak window.
	var startOK, endOK bool
	if raw := requireEnv("OFFPEAK_START", &errs); raw != "" {
		d, err := parseHHMM(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("OFFPEAK_START: %w", err))
		} else {
			cfg.OffpeakStart = d
			startOK = true
		}
	}

	if raw := requireEnv("OFFPEAK_END", &errs); raw != "" {
		d, err := parseHHMM(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("OFFPEAK_END: %w", err))
		} else {
			cfg.OffpeakEnd = d
			endOK = true
		}
	}

	if startOK && endOK && cfg.OffpeakStart >= cfg.OffpeakEnd {
		errs = append(errs, fmt.Errorf("OFFPEAK_START must be before OFFPEAK_END (%s >= %s)",
			FormatHHMM(cfg.OffpeakStart), FormatHHMM(cfg.OffpeakEnd)))
	}

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
		"offpeak", FormatHHMM(cfg.OffpeakStart)+"-"+FormatHHMM(cfg.OffpeakEnd),
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

// parseHHMM parses a "HH:MM" string into a time.Duration from midnight.
func parseHHMM(s string) (time.Duration, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected HH:MM format, got %q", s)
	}

	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour in %q", s)
	}

	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minute in %q", s)
	}

	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

// SocAlertsConfigured reports whether all SoC alert env vars are present.
// When false, the poller starts without the alert pipeline — useful for
// the gradual rollout described in design.md §Deploy ordering.
func (c *Config) SocAlertsConfigured() bool {
	return c.TableDevices != "" && c.TableSocRules != "" && c.TableSocFireState != "" &&
		c.APNsKeyParam != "" && c.APNsKeyIDParam != "" && c.APNsTeamIDParam != "" &&
		c.APNsBundleIDParam != ""
}

// FormatHHMM formats a duration-from-midnight back to HH:MM for logging.
func FormatHHMM(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%02d:%02d", h, m)
}
