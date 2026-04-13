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
	logger.Info("poller starting",
		"serial", cfg.Serial,
		"offpeak", config.FormatHHMM(cfg.OffpeakStart)+"-"+config.FormatHHMM(cfg.OffpeakEnd),
		"tz", cfg.Location.String(),
		"dry_run", cfg.DryRun,
	)
}
