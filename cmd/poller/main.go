// Package main is the entrypoint for the Flux poller.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "time/tzdata" // Embed timezone data for distroless containers.

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/poller"
)

func main() {
	// Configure structured JSON logging with renamed fields.
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
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
	slog.SetDefault(slog.New(handler))

	// Healthcheck subcommand — fast path, no full startup.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthCheck())
	}

	// Load and validate configuration.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	// Create AlphaESS client.
	client := alphaess.NewClient(cfg.AppID, cfg.AppSecret, cfg.HTTPTimeout)

	// Create store (DynamoDB or dry-run logger).
	store, err := createStore(cfg)
	if err != nil {
		slog.Error("create store failed", "error", err)
		os.Exit(1)
	}

	// Log startup.
	slog.Info("poller starting",
		"serial", cfg.Serial,
		"offpeak", formatHHMM(cfg.OffpeakStart)+"-"+formatHHMM(cfg.OffpeakEnd),
		"tz", cfg.Location.String(),
		"dry_run", cfg.DryRun,
	)

	// Signal handling — SIGTERM/SIGINT cancel the context.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Run poller (blocks until ctx is cancelled).
	p := poller.New(client, store, cfg)
	if err := p.Run(ctx); err != nil {
		slog.Error("poller stopped with error", "error", err)
		os.Exit(1)
	}

	slog.Info("poller stopped")
}

// createStore builds the appropriate Store implementation based on config.
func createStore(cfg *config.Config) (dynamo.Store, error) {
	if cfg.DryRun {
		slog.Info("dry-run mode active, DynamoDB writes disabled")
		return dynamo.NewLogStore(slog.Default()), nil
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.AWSRegion),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := dynamodb.NewFromConfig(awsCfg)
	return dynamo.NewDynamoStore(client, dynamo.TableNames{
		Readings:    cfg.TableReadings,
		DailyEnergy: cfg.TableDailyEnergy,
		DailyPower:  cfg.TableDailyPower,
		System:      cfg.TableSystem,
		Offpeak:     cfg.TableOffpeak,
	}), nil
}

// formatHHMM formats a duration-from-midnight as HH:MM.
func formatHHMM(d time.Duration) string {
	return fmt.Sprintf("%02d:%02d", int(d.Hours()), int(d.Minutes())%60)
}
