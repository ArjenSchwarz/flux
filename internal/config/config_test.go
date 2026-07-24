package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fullEnv returns a complete set of valid environment variables.
func fullEnv() map[string]string {
	return map[string]string{
		"ALPHA_APP_ID":       "test-app-id",
		"ALPHA_APP_SECRET":   "test-app-secret",
		"SYSTEM_SERIAL":      "AB1234",
		"AWS_REGION":         "ap-southeast-2",
		"TABLE_READINGS":     "flux-readings",
		"TABLE_DAILY_ENERGY": "flux-daily-energy",
		"TABLE_DAILY_POWER":  "flux-daily-power",
		"TABLE_SYSTEM":       "flux-system",
		"TABLE_OFFPEAK":      "flux-offpeak",
		"TABLE_PRICING":      "flux-pricing",
	}
}

// setEnv sets all env vars from the map using t.Setenv (auto-cleaned up).
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	env := fullEnv()
	setEnv(t, env)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "test-app-id", cfg.AppID)
	assert.Equal(t, "test-app-secret", cfg.AppSecret)
	assert.Equal(t, "AB1234", cfg.Serial)
	assert.Equal(t, "flux-pricing", cfg.TablePricing)
	assert.Equal(t, "ap-southeast-2", cfg.AWSRegion)
	assert.Equal(t, "flux-readings", cfg.TableReadings)
	assert.Equal(t, "flux-daily-energy", cfg.TableDailyEnergy)
	assert.Equal(t, "flux-daily-power", cfg.TableDailyPower)
	assert.Equal(t, "flux-system", cfg.TableSystem)
	assert.Equal(t, "flux-offpeak", cfg.TableOffpeak)
	assert.False(t, cfg.DryRun)
	assert.Equal(t, 10*time.Second, cfg.HTTPTimeout)
}

func TestLoad_DefaultTimezone(t *testing.T) {
	env := fullEnv()
	setEnv(t, env)
	// TZ not set — should default to Australia/Sydney

	cfg, err := Load()
	require.NoError(t, err)

	sydney, _ := time.LoadLocation("Australia/Sydney")
	assert.Equal(t, sydney, cfg.Location)
}

func TestLoad_CustomTimezone(t *testing.T) {
	env := fullEnv()
	env["TZ"] = "America/New_York"
	setEnv(t, env)

	cfg, err := Load()
	require.NoError(t, err)

	ny, _ := time.LoadLocation("America/New_York")
	assert.Equal(t, ny, cfg.Location)
}

func TestLoad_MissingRequiredVars(t *testing.T) {
	requiredVars := []string{
		"ALPHA_APP_ID",
		"ALPHA_APP_SECRET",
		"SYSTEM_SERIAL",
		"AWS_REGION",
		"TABLE_READINGS",
		"TABLE_DAILY_ENERGY",
		"TABLE_DAILY_POWER",
		"TABLE_SYSTEM",
		"TABLE_OFFPEAK",
		"TABLE_PRICING",
	}

	for _, varName := range requiredVars {
		t.Run(varName, func(t *testing.T) {
			env := fullEnv()
			delete(env, varName)
			setEnv(t, env)

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), varName)
		})
	}
}

// The off-peak window is no longer configuration: it comes from the plan
// pricing each day (Decision 2), so there is nothing here to validate.

func TestLoad_InvalidTimezone(t *testing.T) {
	env := fullEnv()
	env["TZ"] = "Not/A/Timezone"
	setEnv(t, env)

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TZ")
}

func TestLoad_DryRunRelaxesAWSVars(t *testing.T) {
	// In dry-run mode, AWS_REGION and TABLE_* vars are not required.
	env := map[string]string{
		"DRY_RUN":          "true",
		"ALPHA_APP_ID":     "test-app-id",
		"ALPHA_APP_SECRET": "test-app-secret",
		"SYSTEM_SERIAL":    "AB1234",
	}
	setEnv(t, env)

	cfg, err := Load()
	require.NoError(t, err)

	assert.True(t, cfg.DryRun)
	assert.Empty(t, cfg.AWSRegion)
	assert.Empty(t, cfg.TableReadings)
}

func TestLoad_DryRunStillRequiresAlphaVars(t *testing.T) {
	// Even in dry-run mode, AlphaESS credentials and serial are required.
	tests := map[string]string{
		"ALPHA_APP_ID":     "ALPHA_APP_ID",
		"ALPHA_APP_SECRET": "ALPHA_APP_SECRET",
		"SYSTEM_SERIAL":    "SYSTEM_SERIAL",
	}

	for missing, errOn := range tests {
		t.Run(missing, func(t *testing.T) {
			env := map[string]string{
				"DRY_RUN":          "true",
				"ALPHA_APP_ID":     "test-app-id",
				"ALPHA_APP_SECRET": "test-app-secret",
				"SYSTEM_SERIAL":    "AB1234",
			}
			delete(env, missing)
			setEnv(t, env)

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), errOn)
		})
	}
}

func TestLoad_CollectsMultipleErrors(t *testing.T) {
	// When multiple vars are missing, all should be reported.
	// Set only DRY_RUN — missing every AlphaESS var.
	t.Setenv("DRY_RUN", "false")
	// Don't set any other vars.

	_, err := Load()
	require.Error(t, err)

	// Should mention at least these missing vars.
	assert.Contains(t, err.Error(), "ALPHA_APP_ID")
	assert.Contains(t, err.Error(), "ALPHA_APP_SECRET")
	assert.Contains(t, err.Error(), "SYSTEM_SERIAL")
}
