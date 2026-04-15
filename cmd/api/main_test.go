package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigMissingEnv(t *testing.T) {
	// Set all env vars except TABLE_READINGS to verify validation catches it.
	t.Setenv("TABLE_READINGS", "")
	t.Setenv("TABLE_DAILY_ENERGY", "flux-daily-energy")
	t.Setenv("TABLE_DAILY_POWER", "flux-daily-power")
	t.Setenv("TABLE_SYSTEM", "flux-system")
	t.Setenv("TABLE_OFFPEAK", "flux-offpeak")
	t.Setenv("OFFPEAK_START", "11:00")
	t.Setenv("OFFPEAK_END", "14:00")
	t.Setenv("API_TOKEN_PARAM", "/flux/api-token")
	t.Setenv("SYSTEM_SERIAL_PARAM", "/flux/system-serial")

	_, err := loadConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TABLE_READINGS")
}

func TestLoadConfigAllEnvMissing(t *testing.T) {
	// Clear all required env vars.
	for _, key := range requiredEnvVars {
		t.Setenv(key, "")
	}

	_, err := loadConfig(context.Background())
	require.Error(t, err)
	// Should fail on the first missing var.
	assert.Contains(t, err.Error(), "missing required environment variable")
}
