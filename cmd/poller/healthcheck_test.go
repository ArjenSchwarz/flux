package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
)

// mockHealthQuery implements healthQueryAPI for testing.
type mockHealthQuery struct {
	output *dynamodb.QueryOutput
	err    error
}

func (m *mockHealthQuery) Query(_ context.Context, _ *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	return m.output, m.err
}

func TestCheckHealth_RecentReading_ReturnsHealthy(t *testing.T) {
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	recentTS := now.Add(-30 * time.Second).Unix() // 30s ago

	mock := &mockHealthQuery{
		output: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{
				{"timestamp": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", recentTS)}},
			},
		},
	}

	got := checkHealth(context.Background(), mock, "flux-readings", "TEST123", func() time.Time { return now })
	assert.Equal(t, 0, got)
}

func TestCheckHealth_StaleReading_ReturnsUnhealthy(t *testing.T) {
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	staleTS := now.Add(-120 * time.Second).Unix() // 120s ago

	mock := &mockHealthQuery{
		output: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{
				{"timestamp": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", staleTS)}},
			},
		},
	}

	got := checkHealth(context.Background(), mock, "flux-readings", "TEST123", func() time.Time { return now })
	assert.Equal(t, 1, got)
}

func TestCheckHealth_NoReadings_ReturnsUnhealthy(t *testing.T) {
	mock := &mockHealthQuery{
		output: &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{}},
	}

	got := checkHealth(context.Background(), mock, "flux-readings", "TEST123", time.Now)
	assert.Equal(t, 1, got)
}

func TestCheckHealth_QueryError_ReturnsUnhealthy(t *testing.T) {
	mock := &mockHealthQuery{err: errors.New("connection refused")}

	got := checkHealth(context.Background(), mock, "flux-readings", "TEST123", time.Now)
	assert.Equal(t, 1, got)
}

func TestCheckHealth_ExactlyAt60Seconds_ReturnsHealthy(t *testing.T) {
	// Requirement: <60s → healthy, >60s → unhealthy. Exactly 60s is not "more than 60".
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	borderTS := now.Add(-60 * time.Second).Unix()

	mock := &mockHealthQuery{
		output: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{
				{"timestamp": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", borderTS)}},
			},
		},
	}

	got := checkHealth(context.Background(), mock, "flux-readings", "TEST123", func() time.Time { return now })
	assert.Equal(t, 0, got, "exactly 60s is not 'more than 60', so healthy")
}

func TestCheckHealth_At61Seconds_ReturnsUnhealthy(t *testing.T) {
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	staleTS := now.Add(-61 * time.Second).Unix()

	mock := &mockHealthQuery{
		output: &dynamodb.QueryOutput{
			Items: []map[string]types.AttributeValue{
				{"timestamp": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", staleTS)}},
			},
		},
	}

	got := checkHealth(context.Background(), mock, "flux-readings", "TEST123", func() time.Time { return now })
	assert.Equal(t, 1, got)
}

func TestRunHealthCheck_DryRun_ReturnsHealthy(t *testing.T) {
	t.Setenv("DRY_RUN", "true")
	got := runHealthCheck()
	assert.Equal(t, 0, got)
}

func TestRunHealthCheck_MissingEnvVars_ReturnsUnhealthy(t *testing.T) {
	// Ensure DRY_RUN is not set, and required vars are missing.
	t.Setenv("DRY_RUN", "false")
	t.Setenv("AWS_REGION", "")
	t.Setenv("TABLE_READINGS", "")
	t.Setenv("SYSTEM_SERIAL", "")

	got := runHealthCheck()
	assert.Equal(t, 1, got)
}
