package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/config"
)

func TestNewJSONLoggerUsesTimestampAndLowercaseLevel(t *testing.T) {
	var output bytes.Buffer
	logger := newJSONLogger(&output)
	logger.Warn("logger check")

	logLine := output.String()
	if !strings.Contains(logLine, `"timestamp":`) {
		t.Fatalf("expected timestamp field in log output, got %q", logLine)
	}
	if !strings.Contains(logLine, `"level":"warn"`) {
		t.Fatalf("expected lowercase warn level in log output, got %q", logLine)
	}
	if strings.Contains(logLine, `"time":`) {
		t.Fatalf("did not expect time field in log output, got %q", logLine)
	}
}

func TestLogPollerStartupDoesNotLogAppSecret(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	cfg := &config.Config{
		Serial:       "SYS-001",
		AppSecret:    "super-secret-value",
		OffpeakStart: 11 * time.Hour,
		OffpeakEnd:   14 * time.Hour,
		Location:     time.UTC,
		DryRun:       true,
	}

	logPollerStartup(cfg, logger)
	logOutput := output.String()
	if strings.Contains(logOutput, cfg.AppSecret) {
		t.Fatalf("startup logs must not include app secret, got %q", logOutput)
	}
	if !strings.Contains(logOutput, `"serial":"SYS-001"`) {
		t.Fatalf("expected serial to be logged, got %q", logOutput)
	}
}
