package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// healthQueryAPI is the subset of the DynamoDB client used by the health check.
type healthQueryAPI interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// runHealthCheck checks whether the poller is healthy by querying for a recent reading.
// In dry-run mode, it always returns 0 (healthy).
func runHealthCheck() int {
	if os.Getenv("DRY_RUN") == "true" {
		slog.Info("healthcheck: dry-run mode, reporting healthy")
		return 0
	}

	region := os.Getenv("AWS_REGION")
	table := os.Getenv("TABLE_READINGS")
	serial := os.Getenv("SYSTEM_SERIAL")

	if region == "" || table == "" || serial == "" {
		slog.Error("healthcheck: missing required env vars",
			"AWS_REGION", region != "",
			"TABLE_READINGS", table != "",
			"SYSTEM_SERIAL", serial != "",
		)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		slog.Error("healthcheck: load AWS config", "error", err)
		return 1
	}

	client := dynamodb.NewFromConfig(cfg)
	return checkHealth(ctx, client, table, serial, time.Now)
}

// checkHealth queries DynamoDB for the most recent reading and returns 0 if it's
// less than 60 seconds old, 1 otherwise. Extracted for testability.
func checkHealth(ctx context.Context, client healthQueryAPI, table, serial string, now func() time.Time) int {
	out, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              &table,
		KeyConditionExpression: aws.String("sysSn = :sn"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sn": &types.AttributeValueMemberS{Value: serial},
		},
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(1),
	})
	if err != nil {
		slog.Error("healthcheck: query failed", "table", table, "error", err)
		return 1
	}

	if len(out.Items) == 0 {
		slog.Warn("healthcheck: no readings found", "serial", serial)
		return 1
	}

	tsAttr, ok := out.Items[0]["timestamp"]
	if !ok {
		slog.Error("healthcheck: reading missing timestamp field")
		return 1
	}

	tsNum, ok := tsAttr.(*types.AttributeValueMemberN)
	if !ok {
		slog.Error("healthcheck: timestamp is not a number")
		return 1
	}

	ts, err := strconv.ParseInt(tsNum.Value, 10, 64)
	if err != nil {
		slog.Error("healthcheck: parse timestamp", "value", tsNum.Value, "error", err)
		return 1
	}

	age := now().Unix() - ts
	if age > 60 {
		slog.Warn("healthcheck: reading is stale", "age_seconds", age)
		return 1
	}

	slog.Debug("healthcheck: healthy", "age_seconds", age)
	return 0
}
