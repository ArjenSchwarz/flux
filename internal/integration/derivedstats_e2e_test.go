// Package integration holds end-to-end tests that exercise real DynamoDB
// via DynamoDB Local. Gated on INTEGRATION=1 per the project's testing
// rules; default `go test ./...` skips the package.
//
// To run locally:
//
//	docker run -d -p 8000:8000 --name dynamodb-local amazon/dynamodb-local
//	INTEGRATION=1 DYNAMODB_LOCAL_ENDPOINT=http://localhost:8000 go test ./internal/integration/...
//
// CI is expected to start DynamoDB Local via Testcontainers (or an ephemeral
// container in the workflow) and set DYNAMODB_LOCAL_ENDPOINT for the test
// run; the test does not currently spin up its own container, but is
// structured so that switching to a Testcontainers-managed launch is a
// localised change.
package integration

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/require"

	"github.com/ArjenSchwarz/flux/internal/api"
	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/poller"
)

// TestEndToEnd_DerivedStatsRoundTrip exercises the full poller→DynamoDB→Lambda
// path against a real DynamoDB surface (DynamoDB Local), per AC 6.7. It
// stages readings + an existing energy row, drives the poller's
// summarisation pass, and then invokes the Lambda /day and /history handlers
// to confirm the stored derivedStats survive the round-trip.
func TestEndToEnd_DerivedStatsRoundTrip(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run")
	}
	endpoint := os.Getenv("DYNAMODB_LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8000"
	}

	ctx := context.Background()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("ap-southeast-2"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("local", "local", "")),
	)
	require.NoError(t, err)

	client := dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	tables := dynamo.TableNames{
		Readings:    "flux-readings-itest",
		DailyEnergy: "flux-daily-energy-itest",
		DailyPower:  "flux-daily-power-itest",
		System:      "flux-system-itest",
		Offpeak:     "flux-offpeak-itest",
		Notes:       "flux-notes-itest",
	}
	for _, def := range []struct {
		name      string
		hashKey   string
		rangeKey  string
		hashType  types.ScalarAttributeType
		rangeType types.ScalarAttributeType
	}{
		{tables.Readings, "sysSn", "timestamp", types.ScalarAttributeTypeS, types.ScalarAttributeTypeN},
		{tables.DailyEnergy, "sysSn", "date", types.ScalarAttributeTypeS, types.ScalarAttributeTypeS},
		{tables.DailyPower, "sysSn", "uploadTime", types.ScalarAttributeTypeS, types.ScalarAttributeTypeS},
		{tables.System, "sysSn", "", types.ScalarAttributeTypeS, ""},
		{tables.Offpeak, "sysSn", "date", types.ScalarAttributeTypeS, types.ScalarAttributeTypeS},
		{tables.Notes, "sysSn", "date", types.ScalarAttributeTypeS, types.ScalarAttributeTypeS},
	} {
		_, _ = client.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(def.name)})
		input := &dynamodb.CreateTableInput{
			TableName:   aws.String(def.name),
			BillingMode: types.BillingModePayPerRequest,
			AttributeDefinitions: []types.AttributeDefinition{
				{AttributeName: aws.String(def.hashKey), AttributeType: def.hashType},
			},
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String(def.hashKey), KeyType: types.KeyTypeHash},
			},
		}
		if def.rangeKey != "" {
			input.AttributeDefinitions = append(input.AttributeDefinitions, types.AttributeDefinition{
				AttributeName: aws.String(def.rangeKey), AttributeType: def.rangeType,
			})
			input.KeySchema = append(input.KeySchema, types.KeySchemaElement{
				AttributeName: aws.String(def.rangeKey), KeyType: types.KeyTypeRange,
			})
		}
		_, err := client.CreateTable(ctx, input)
		require.NoError(t, err)
	}

	store := dynamo.NewDynamoStore(client, tables)
	reader := dynamo.NewDynamoReader(client, tables)

	// Stage a fixture day's readings + an existing energy row.
	const serial = "TEST123"
	const date = "2026-04-14"
	loc, _ := time.LoadLocation("Australia/Sydney")
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	for i := range 24 * 60 {
		ts := dayStart.Add(time.Duration(i) * time.Minute).Unix()
		require.NoError(t, store.WriteReading(ctx, dynamo.ReadingItem{
			SysSn:     serial,
			Timestamp: ts,
			Ppv:       1500,
			Pload:     800,
			Soc:       50,
		}))
	}
	require.NoError(t, store.WriteDailyEnergy(ctx, dynamo.DailyEnergyItem{
		SysSn: serial, Date: date,
		Epv: 12.0, EInput: 3.0, EOutput: 1.0, ECharge: 6.0, EDischarge: 5.0,
	}))

	// Build a poller with the clock pinned so SummariseYesterday targets the
	// fixture date, then run the actual summarisation pass — this exercises
	// the writer side of the AC 6.7 contract end-to-end (no hand-rolled
	// payload).
	cfg := &config.Config{
		Serial:       serial,
		Location:     loc,
		OffpeakStart: 11 * time.Hour,
		OffpeakEnd:   14 * time.Hour,
	}
	p := poller.New(nil, store, cfg)
	p.SetMetrics(poller.NoopMetrics{})
	// 02:00 AEST on 2026-04-15 ⇒ yesterday-in-Sydney = 2026-04-14.
	p.SetNow(func() time.Time { return time.Date(2026, 4, 15, 2, 0, 0, 0, loc) })
	p.SummariseYesterday(ctx)

	// Verify storage round-trip — the row now carries all four derived
	// attributes, written by the real poller pass against real DynamoDB.
	row, err := store.GetDailyEnergy(ctx, serial, date)
	require.NoError(t, err)
	require.NotNil(t, row)
	require.NotNil(t, row.DailyUsage, "DailyUsage must round-trip via DynamoDB Local")
	require.NotEmpty(t, row.DailyUsage.Blocks, "summarisation pass should produce at least one block")
	require.NotNil(t, row.SocLow, "SocLow must round-trip")
	require.NotEmpty(t, row.DerivedStatsComputedAt, "sentinel must be written")

	// SolarKwh round-trip: the synthetic readings carry Ppv=1500W all day, so
	// every daylight block must persist a non-nil SolarKwh that survives
	// MarshalMap→DynamoDB Local→Unmarshal. Night/evening must remain nil per
	// Decision 1.
	storedByKind := map[string]dynamo.DailyUsageBlockAttr{}
	for _, b := range row.DailyUsage.Blocks {
		storedByKind[b.Kind] = b
	}
	for _, kind := range []string{
		derivedstats.DailyUsageKindMorningPeak,
		derivedstats.DailyUsageKindOffPeak,
		derivedstats.DailyUsageKindAfternoonPeak,
	} {
		b, ok := storedByKind[kind]
		require.True(t, ok, "%s block must be in stored row", kind)
		require.NotNil(t, b.SolarKwh, "%s SolarKwh must round-trip via DynamoDB Local", kind)
		require.Greater(t, *b.SolarKwh, 0.0)
	}
	for _, kind := range []string{
		derivedstats.DailyUsageKindNight,
		derivedstats.DailyUsageKindEvening,
	} {
		if b, ok := storedByKind[kind]; ok {
			require.Nil(t, b.SolarKwh, "%s block must not carry SolarKwh", kind)
		}
	}

	// Verify the Lambda /day handler reads the row and surfaces the
	// derivedStats sections to clients (read side of the AC 6.7 contract).
	const apiToken = "test-token"
	h := api.NewHandler(reader, nil, serial, apiToken)
	h.SetPricingStore(fixedPlanStore{})
	// 12:00 AEST on 2026-04-15 ⇒ /day for 2026-04-14 takes the past-date
	// branch (reads from storage, not the readings table).
	h.SetNow(func() time.Time { return time.Date(2026, 4, 15, 12, 0, 0, 0, loc) })

	dayResp, err := h.Handle(ctx, events.LambdaFunctionURLRequest{
		QueryStringParameters: map[string]string{"date": date},
		Headers:               map[string]string{"authorization": "Bearer " + apiToken},
		RawPath:               "/day",
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: "GET"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 200, dayResp.StatusCode, "/day must return 200; body=%s", dayResp.Body)

	var dayBody api.DayDetailResponse
	require.NoError(t, json.Unmarshal([]byte(dayResp.Body), &dayBody))
	require.NotNil(t, dayBody.DailyUsage, "/day must surface dailyUsage from storage")
	require.NotEmpty(t, dayBody.DailyUsage.Blocks)
	require.NotNil(t, dayBody.Summary, "/day summary must include socLow")
	require.NotNil(t, dayBody.Summary.SocLow, "summary.socLow must be populated from storage")

	dayByKind := map[string]derivedstats.DailyUsageBlock{}
	for _, b := range dayBody.DailyUsage.Blocks {
		dayByKind[b.Kind] = b
	}
	for _, kind := range []string{
		derivedstats.DailyUsageKindMorningPeak,
		derivedstats.DailyUsageKindOffPeak,
		derivedstats.DailyUsageKindAfternoonPeak,
	} {
		b, ok := dayByKind[kind]
		require.True(t, ok, "/day must include %s block", kind)
		require.NotNil(t, b.SolarKwh, "/day must surface SolarKwh on %s", kind)
		require.InDelta(t, *storedByKind[kind].SolarKwh, *b.SolarKwh, 1e-9, "/day SolarKwh must match storage for %s", kind)
	}

	// Verify /history surfaces derivedStats per row.
	historyResp, err := h.Handle(ctx, events.LambdaFunctionURLRequest{
		QueryStringParameters: map[string]string{"days": "2"},
		Headers:               map[string]string{"authorization": "Bearer " + apiToken},
		RawPath:               "/history",
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: "GET"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 200, historyResp.StatusCode, "/history must return 200; body=%s", historyResp.Body)

	var historyBody api.HistoryResponse
	require.NoError(t, json.Unmarshal([]byte(historyResp.Body), &historyBody))
	var fixtureDay *api.DayEnergy
	for i := range historyBody.Days {
		if historyBody.Days[i].Date == date {
			fixtureDay = &historyBody.Days[i]
			break
		}
	}
	require.NotNil(t, fixtureDay, "/history must include the fixture date in its range")
	require.NotNil(t, fixtureDay.DailyUsage, "/history past-date row must surface dailyUsage from storage")
	require.NotEmpty(t, fixtureDay.DailyUsage.Blocks)
	require.NotNil(t, fixtureDay.SocLow, "/history past-date row must surface flat socLow from storage")

	historyByKind := map[string]derivedstats.DailyUsageBlock{}
	for _, b := range fixtureDay.DailyUsage.Blocks {
		historyByKind[b.Kind] = b
	}
	for _, kind := range []string{
		derivedstats.DailyUsageKindMorningPeak,
		derivedstats.DailyUsageKindOffPeak,
		derivedstats.DailyUsageKindAfternoonPeak,
	} {
		b, ok := historyByKind[kind]
		require.True(t, ok, "/history must include %s block", kind)
		require.NotNil(t, b.SolarKwh, "/history must surface SolarKwh on %s", kind)
		require.InDelta(t, *storedByKind[kind].SolarKwh, *b.SolarKwh, 1e-9, "/history SolarKwh must match storage for %s", kind)
	}
}

// fixedPlanStore is a read-only api.PricingStore serving one open-ended plan
// with the free window this test's fixtures were computed under. The Lambda
// derives the off-peak window from plans, so /day and /history need a plan;
// the pricing table itself is not what this test exercises, so the store is
// stubbed rather than provisioned in DynamoDB Local.
type fixedPlanStore struct{}

func (fixedPlanStore) ListPricing(context.Context) ([]dynamo.PricingItem, error) {
	savings := 0.15
	return []dynamo.PricingItem{{
		PricingID:            "e2e-plan",
		StartDate:            "2000-01-01",
		DefaultRate:          0.3,
		Windows:              []dynamo.PricingWindow{{Start: "11:00", End: "14:00", Free: true}},
		FeedInRate:           0.05,
		SavingsReferenceRate: &savings,
	}}, nil
}

func (fixedPlanStore) GetPricing(context.Context, string) (*dynamo.PricingItem, error) {
	return nil, nil
}

func (fixedPlanStore) GetSentinel(context.Context) (*dynamo.PricingSentinel, error) {
	return nil, nil
}

func (fixedPlanStore) PutPricing(context.Context, dynamo.PricingItem, *string) error { return nil }

func (fixedPlanStore) UpdatePricing(context.Context, dynamo.PricingItem, *string) error { return nil }

func (fixedPlanStore) DeletePricing(context.Context, string, *string) error { return nil }

func (fixedPlanStore) ReplaceOpenEnded(context.Context, string, string, string, dynamo.PricingItem) error {
	return nil
}
