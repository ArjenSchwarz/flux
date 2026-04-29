package poller

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCloudWatch records calls to PutMetricData.
type fakeCloudWatch struct {
	putMetricFn func(ctx context.Context, in *cloudwatch.PutMetricDataInput) (*cloudwatch.PutMetricDataOutput, error)
	calls       []*cloudwatch.PutMetricDataInput
}

func (f *fakeCloudWatch) PutMetricData(ctx context.Context, in *cloudwatch.PutMetricDataInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.PutMetricDataOutput, error) {
	f.calls = append(f.calls, in)
	if f.putMetricFn != nil {
		return f.putMetricFn(ctx, in)
	}
	return &cloudwatch.PutMetricDataOutput{}, nil
}

func TestMetrics_RecordSummarisationPass_EmitsCorrectShape(t *testing.T) {
	results := []string{
		"success",
		"skipped-no-readings",
		"skipped-no-row",
		"skipped-ssm-unresolved",
		"skipped-already-populated",
		"error",
	}

	for _, result := range results {
		t.Run(result, func(t *testing.T) {
			fake := &fakeCloudWatch{}
			m := NewMetrics(fake)

			m.RecordSummarisationPass(context.Background(), result)

			require.Len(t, fake.calls, 1)
			in := fake.calls[0]
			require.NotNil(t, in.Namespace)
			assert.Equal(t, "Flux/Poller", *in.Namespace)
			require.Len(t, in.MetricData, 1)
			datum := in.MetricData[0]
			require.NotNil(t, datum.MetricName)
			assert.Equal(t, "SummarisationPassResult", *datum.MetricName)
			require.Len(t, datum.Dimensions, 1)
			assert.Equal(t, "Result", aws.ToString(datum.Dimensions[0].Name))
			assert.Equal(t, result, aws.ToString(datum.Dimensions[0].Value))
			assert.Equal(t, types.StandardUnitCount, datum.Unit)
		})
	}
}

func TestMetrics_RecordSummarisationPass_PutFailureNotPropagated(t *testing.T) {
	fake := &fakeCloudWatch{
		putMetricFn: func(_ context.Context, _ *cloudwatch.PutMetricDataInput) (*cloudwatch.PutMetricDataOutput, error) {
			return nil, errors.New("throttled")
		},
	}
	m := NewMetrics(fake)
	// Should not panic and should not return anything (logs warn).
	m.RecordSummarisationPass(context.Background(), "success")
	assert.Len(t, fake.calls, 1)
}

func TestMetrics_DryRunNoOp_MakesNoCalls(t *testing.T) {
	fake := &fakeCloudWatch{}
	// NoopMetrics is a stub used when cfg.DryRun is set.
	m := NoopMetrics{}
	m.RecordSummarisationPass(context.Background(), "success")
	assert.Empty(t, fake.calls, "no calls should be issued in dry-run mode")
}
