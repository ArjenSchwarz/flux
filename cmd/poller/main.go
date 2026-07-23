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

	// Create store (DynamoDB or dry-run logger) and the read-only pricing
	// source the window-dependent jobs resolve their day's free band from.
	store, plans, err := createStore(cfg)
	if err != nil {
		slog.Error("create store failed", "error", err)
		os.Exit(1)
	}

	logPollerStartup(cfg, slog.Default())

	// Signal handling — SIGTERM/SIGINT cancel the context.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Run poller (blocks until ctx is cancelled).
	p := poller.New(client, store, plans, cfg)

	// CloudWatch metrics for the daily-derived-stats summarisation pass.
	// Dry-run keeps the no-op variant set by poller.New.
	if !cfg.DryRun {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWSRegion))
		if err != nil {
			slog.Error("load AWS config for cloudwatch", "error", err)
			os.Exit(1)
		}
		p.SetMetrics(poller.NewMetrics(cloudwatch.NewFromConfig(awsCfg)))

		if cfg.SocAlertsConfigured() {
			if err := wireSocAlerts(ctx, p, cfg, awsCfg); err != nil {
				slog.Error("wire soc alerts failed", "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("soc alerts not configured; skipping APNs wiring")
		}
	}

	if err := p.Run(ctx); err != nil {
		slog.Error("poller stopped with error", "error", err)
		os.Exit(1)
	}

	slog.Info("poller stopped")
}

// createStore builds the Store and the read-only pricing source from config.
// Both come from the same DynamoDB client, so the pricing reader is created
// here rather than making the caller rebuild the AWS config.
func createStore(cfg *config.Config) (dynamo.Store, poller.PlanLister, error) {
	if cfg.DryRun {
		slog.Info("dry-run mode active, DynamoDB writes disabled; no pricing plans, so window-dependent jobs stay idle")
		return dynamo.NewLogStore(slog.Default()), noPlans{}, nil
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.AWSRegion),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := dynamodb.NewFromConfig(awsCfg)
	store := dynamo.NewDynamoStore(client, dynamo.TableNames{
		Readings:    cfg.TableReadings,
		DailyEnergy: cfg.TableDailyEnergy,
		DailyPower:  cfg.TableDailyPower,
		System:      cfg.TableSystem,
		Offpeak:     cfg.TableOffpeak,
	})
	return store, dynamo.NewDynamoPricingStore(client, cfg.TablePricing), nil
}

// noPlans is the dry-run pricing source. Dry-run has no DynamoDB credentials
// or table names, so it reports an empty plan set — which the scheduler and
// the summarisation pass both treat as "no plan prices this day" and skip,
// exactly the no-write behaviour dry-run is for.
type noPlans struct{}

func (noPlans) ListPricing(context.Context) ([]dynamo.PricingItem, error) { return nil, nil }
