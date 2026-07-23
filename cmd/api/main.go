// Package main is the entrypoint for the Flux Lambda API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	_ "time/tzdata" // Embed timezone data for the provided.al2023 runtime.

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/ArjenSchwarz/flux/internal/api"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/lambda"
)

// config holds all resolved configuration for the Lambda.
type config struct {
	reader    dynamo.Reader
	notes     api.NoteWriter
	devices   api.DeviceStore
	rules     api.SocRuleStore
	fireState api.FireStateCleaner
	pricing   api.PricingStore
	presets   api.SimulationPresetStore
	apiToken  string
	serial    string
}

// requiredEnvVars lists environment variables that must be set.
var requiredEnvVars = []string{
	"TABLE_READINGS",
	"TABLE_DAILY_ENERGY",
	"TABLE_DAILY_POWER",
	"TABLE_SYSTEM",
	"TABLE_OFFPEAK",
	"TABLE_NOTES",
	"TABLE_DEVICES",
	"TABLE_SOC_RULES",
	"TABLE_SOC_FIRESTATE",
	"TABLE_PRICING",
	"TABLE_SIMULATION_PRESETS",
	"API_TOKEN_PARAM",
	"SYSTEM_SERIAL_PARAM",
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	ctx := context.Background()
	cfg, err := loadConfig(ctx)
	if err != nil {
		slog.Error("init failed", "error", err)
		os.Exit(1)
	}

	handler := api.NewHandler(cfg.reader, cfg.notes, cfg.serial, cfg.apiToken)
	handler.SetDeviceStore(cfg.devices)
	handler.SetSocRuleStore(cfg.rules)
	handler.SetFireStateCleaner(cfg.fireState)
	handler.SetPricingStore(cfg.pricing)
	handler.SetSimulationPresetStore(cfg.presets)
	lambda.Start(handler.Handle)
}

// loadConfig loads AWS SDK config, fetches SSM parameters, reads env vars,
// and validates all required configuration is present.
func loadConfig(ctx context.Context) (*config, error) {
	// Validate all required env vars before doing any AWS calls.
	for _, key := range requiredEnvVars {
		if os.Getenv(key) == "" {
			return nil, fmt.Errorf("missing required environment variable: %s", key)
		}
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	// Fetch SSM parameters.
	ssmClient := ssm.NewFromConfig(awsCfg)

	apiToken, err := getSSMParam(ctx, ssmClient, os.Getenv("API_TOKEN_PARAM"))
	if err != nil {
		return nil, fmt.Errorf("load api token: %w", err)
	}

	serial, err := getSSMParam(ctx, ssmClient, os.Getenv("SYSTEM_SERIAL_PARAM"))
	if err != nil {
		return nil, fmt.Errorf("load serial: %w", err)
	}

	// Create DynamoDB reader and note writer. The single client satisfies
	// both ReadAPI and WriteAPI at compile time; there is no actual coupling
	// between the read and write paths.
	ddbClient := dynamodb.NewFromConfig(awsCfg)
	reader := dynamo.NewDynamoReader(ddbClient, dynamo.TableNames{
		Readings:    os.Getenv("TABLE_READINGS"),
		DailyEnergy: os.Getenv("TABLE_DAILY_ENERGY"),
		DailyPower:  os.Getenv("TABLE_DAILY_POWER"),
		System:      os.Getenv("TABLE_SYSTEM"),
		Offpeak:     os.Getenv("TABLE_OFFPEAK"),
		Notes:       os.Getenv("TABLE_NOTES"),
	})
	notes := dynamo.NewDynamoNoteWriter(ddbClient, os.Getenv("TABLE_NOTES"))

	// SoC alert stores. The Dynamo writer/reader concrete types satisfy
	// the api package's DeviceStore / SocRuleStore / FireStateCleaner
	// interfaces directly — the FireStateCleaner adapter only translates
	// the int return of DeleteByDeviceRule into the interface contract.
	devices := dynamo.NewDynamoDeviceWriter(ddbClient, os.Getenv("TABLE_DEVICES"))
	ruleReader := dynamo.NewDynamoSocRuleReader(ddbClient, os.Getenv("TABLE_SOC_RULES"))
	ruleWriter := dynamo.NewDynamoSocRuleWriter(ddbClient, os.Getenv("TABLE_SOC_RULES"))
	fireState := dynamo.NewDynamoSocFireStateWriter(ddbClient, os.Getenv("TABLE_SOC_FIRESTATE"))
	pricing := dynamo.NewDynamoPricingStore(ddbClient, os.Getenv("TABLE_PRICING"))
	presets := dynamo.NewDynamoSimulationPresetStore(ddbClient, os.Getenv("TABLE_SIMULATION_PRESETS"))

	return &config{
		reader:    reader,
		notes:     notes,
		devices:   devices,
		rules:     socRuleStoreAdapter{reader: ruleReader, writer: ruleWriter},
		fireState: fireStateCleanerAdapter{store: fireState},
		pricing:   pricingStoreAdapter{store: pricing},
		presets:   presets,
		apiToken:  apiToken,
		serial:    serial,
	}, nil
}

// socRuleStoreAdapter exposes the (reader, writer) pair as the single
// SocRuleStore interface the handler depends on.
type socRuleStoreAdapter struct {
	reader *dynamo.DynamoSocRuleReader
	writer *dynamo.DynamoSocRuleWriter
}

func (a socRuleStoreAdapter) ListRulesByDevice(ctx context.Context, deviceID string) ([]dynamo.SoCRuleItem, error) {
	return a.reader.ListRulesByDevice(ctx, deviceID)
}

func (a socRuleStoreAdapter) PutRule(ctx context.Context, item dynamo.SoCRuleItem) error {
	return a.writer.PutRule(ctx, item)
}

func (a socRuleStoreAdapter) DeleteRule(ctx context.Context, deviceID, ruleID string) error {
	return a.writer.DeleteRule(ctx, deviceID, ruleID)
}

// fireStateCleanerAdapter wraps DynamoSocFireStateWriter.DeleteByDeviceRule
// into the FireStateCleaner interface. The wrapper is needed only because
// the interface returns the count for observability while the writer's
// method matches in shape but lives in the dynamo package.
type fireStateCleanerAdapter struct {
	store *dynamo.DynamoSocFireStateWriter
}

func (a fireStateCleanerAdapter) DeleteFireStateByDeviceRule(ctx context.Context, deviceID, ruleID string) (int, error) {
	return a.store.DeleteByDeviceRule(ctx, deviceID, ruleID)
}

// pricingStoreAdapter bridges *dynamo.DynamoPricingStore to the
// api.PricingStore interface. Identical shapes — the adapter exists
// because the api package depends only on its local interface so test
// fakes don't import the dynamo writer types.
type pricingStoreAdapter struct {
	store *dynamo.DynamoPricingStore
}

func (a pricingStoreAdapter) ListPricing(ctx context.Context) ([]dynamo.PricingItem, error) {
	return a.store.ListPricing(ctx)
}

func (a pricingStoreAdapter) GetPricing(ctx context.Context, id string) (*dynamo.PricingItem, error) {
	return a.store.GetPricing(ctx, id)
}

func (a pricingStoreAdapter) GetSentinel(ctx context.Context) (*dynamo.PricingSentinel, error) {
	return a.store.GetSentinel(ctx)
}

func (a pricingStoreAdapter) PutPricing(ctx context.Context, item dynamo.PricingItem, prevOpenEndedID *string) error {
	return a.store.PutPricing(ctx, item, prevOpenEndedID)
}

func (a pricingStoreAdapter) UpdatePricing(ctx context.Context, item dynamo.PricingItem, prevOpenEndedID *string) error {
	return a.store.UpdatePricing(ctx, item, prevOpenEndedID)
}

func (a pricingStoreAdapter) DeletePricing(ctx context.Context, id string, prevOpenEndedID *string) error {
	return a.store.DeletePricing(ctx, id, prevOpenEndedID)
}

func (a pricingStoreAdapter) ReplaceOpenEnded(ctx context.Context, closingID, closingEndDate, updatedAt string, newItem dynamo.PricingItem) error {
	return a.store.ReplaceOpenEnded(ctx, closingID, closingEndDate, updatedAt, newItem)
}

// ssmAPI is the subset of the SSM client used for parameter fetching.
type ssmAPI interface {
	GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

// getSSMParam fetches a single SSM parameter value with decryption.
func getSSMParam(ctx context.Context, client ssmAPI, name string) (string, error) {
	decrypt := true
	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &name,
		WithDecryption: &decrypt,
	})
	if err != nil {
		return "", fmt.Errorf("get SSM parameter %q: %w", name, err)
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", fmt.Errorf("SSM parameter %q has no value", name)
	}
	return *out.Parameter.Value, nil
}
