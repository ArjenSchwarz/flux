package main

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/ArjenSchwarz/flux/internal/config"
)

// configureLogger sets up structured JSON logging as the default logger.
func configureLogger() {
	slog.SetDefault(newJSONLogger(os.Stdout))
}

// newJSONLogger creates a JSON logger writing to output with renamed timestamp
// and lowercase level fields.
func newJSONLogger(output io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			if a.Key == slog.LevelKey {
				a.Value = slog.StringValue(strings.ToLower(a.Value.String()))
			}
			return a
		},
	})
	return slog.New(handler)
}

// logPollerStartup logs selected non-secret configuration fields. Never logs
// the full config to avoid leaking AppSecret.
func logPollerStartup(cfg *config.Config, logger *slog.Logger) {
	// The off-peak window is no longer configuration, so it is no longer a
	// startup fact: each day's window is resolved from the plan pricing that
	// day and logged by the scheduler as it processes the day.
	logger.Info("poller starting",
		"serial", cfg.Serial,
		"tz", cfg.Location.String(),
		"dry_run", cfg.DryRun,
	)
}
