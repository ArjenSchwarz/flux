package poller

import (
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

const (
	cloudwatchNamespace              = "Flux/Poller"
	summarisationPassResultMetric    = "SummarisationPassResult"
	summarisationPassDimensionResult = "Result"
)

// SummarisationPassResult dimension values emitted by the daily-derived-stats
// summarisation pass. These map 1:1 to the AC 1.11 dimensions; a CloudWatch
// alarm on absence of `PassResultSuccess` for >24h flags a stuck pass.
const (
	PassResultSuccess              = "success"
	PassResultError                = "error"
	PassResultSkippedNoRow         = "skipped-no-row"
	PassResultSkippedAlreadyDone   = "skipped-already-populated"
	PassResultSkippedSSMUnresolved = "skipped-ssm-unresolved"
	PassResultSkippedNoReadings    = "skipped-no-readings"
)

// MetricsRecorder is the small surface the poller uses for emitting custom
// metrics. Implemented by the production Metrics struct and by NoopMetrics
// for dry-run mode.
type MetricsRecorder interface {
	RecordSummarisationPass(ctx context.Context, result string)
}

// CloudWatchAPI is the subset of the CloudWatch client used by Metrics.
// Defined as an interface so tests can fake PutMetricData without an AWS
// connection.
type CloudWatchAPI interface {
	PutMetricData(ctx context.Context, in *cloudwatch.PutMetricDataInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.PutMetricDataOutput, error)
}

// Metrics emits CloudWatch custom metrics for the poller. Failures to publish
// are logged but never returned: metrics are observability, not load-bearing.
type Metrics struct {
	client CloudWatchAPI
}

// NewMetrics returns a Metrics that publishes to the given CloudWatch client
// under the Flux/Poller namespace.
func NewMetrics(client CloudWatchAPI) *Metrics {
	return &Metrics{client: client}
}

// RecordSummarisationPass emits one SummarisationPassResult data point with
// dimension Result=<result> and unit Count=1. PutMetricData failures are
// logged at warn level and do not affect the caller.
func (m *Metrics) RecordSummarisationPass(ctx context.Context, result string) {
	in := &cloudwatch.PutMetricDataInput{
		Namespace: aws.String(cloudwatchNamespace),
		MetricData: []types.MetricDatum{
			{
				MetricName: aws.String(summarisationPassResultMetric),
				Dimensions: []types.Dimension{
					{
						Name:  aws.String(summarisationPassDimensionResult),
						Value: aws.String(result),
					},
				},
				Unit:  types.StandardUnitCount,
				Value: aws.Float64(1),
			},
		},
	}
	if _, err := m.client.PutMetricData(ctx, in); err != nil {
		slog.Warn("cloudwatch put metric data failed", "metric", summarisationPassResultMetric, "result", result, "error", err)
	}
}

// NoopMetrics is the dry-run / no-AWS variant. It records nothing and makes
// no AWS calls.
type NoopMetrics struct{}

// RecordSummarisationPass on NoopMetrics is a no-op.
func (NoopMetrics) RecordSummarisationPass(_ context.Context, _ string) {}
