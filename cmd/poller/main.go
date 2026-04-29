// Package main is the entrypoint for the Flux poller.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "time/tzdata" // Embed timezone data for distroless containers.

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/poller"
)

func main() {
	configureLogger()

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

	logPollerStartup(cfg, slog.Default())

	// Signal handling — SIGTERM/SIGINT cancel the context.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Run poller (blocks until ctx is cancelled).
	p := poller.New(client, store, cfg)

	// CloudWatch metrics for the daily-derived-stats summarisation pass.
	// Dry-run keeps the no-op variant set by poller.New.
	if !cfg.DryRun {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWSRegion))
		if err != nil {
			slog.Error("load AWS config for cloudwatch", "error", err)
			os.Exit(1)
		}
		p.SetMetrics(poller.NewMetrics(cloudwatch.NewFromConfig(awsCfg)))
	}

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
