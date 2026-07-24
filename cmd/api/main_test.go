package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigMissingEnv(t *testing.T) {
	// Set every required env var except TABLE_READINGS to verify validation
	// catches it. Driven off requiredEnvVars so the list cannot drift out of
	// step with what loadConfig actually demands.
	for _, key := range requiredEnvVars {
		t.Setenv(key, "placeholder")
	}
	t.Setenv("TABLE_READINGS", "")

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
