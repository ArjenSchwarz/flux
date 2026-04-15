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
	reader       dynamo.Reader
	apiToken     string
	serial       string
	offpeakStart string
	offpeakEnd   string
}

// requiredEnvVars lists environment variables that must be set.
var requiredEnvVars = []string{
	"TABLE_READINGS",
	"TABLE_DAILY_ENERGY",
	"TABLE_DAILY_POWER",
	"TABLE_SYSTEM",
	"TABLE_OFFPEAK",
	"OFFPEAK_START",
	"OFFPEAK_END",
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

	handler := api.NewHandler(cfg.reader, cfg.serial, cfg.apiToken, cfg.offpeakStart, cfg.offpeakEnd)
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

	// Create DynamoDB reader.
	ddbClient := dynamodb.NewFromConfig(awsCfg)
	reader := dynamo.NewDynamoReader(ddbClient, dynamo.TableNames{
		Readings:    os.Getenv("TABLE_READINGS"),
		DailyEnergy: os.Getenv("TABLE_DAILY_ENERGY"),
		DailyPower:  os.Getenv("TABLE_DAILY_POWER"),
		System:      os.Getenv("TABLE_SYSTEM"),
		Offpeak:     os.Getenv("TABLE_OFFPEAK"),
	})

	return &config{
		reader:       reader,
		apiToken:     apiToken,
		serial:       serial,
		offpeakStart: os.Getenv("OFFPEAK_START"),
		offpeakEnd:   os.Getenv("OFFPEAK_END"),
	}, nil
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
