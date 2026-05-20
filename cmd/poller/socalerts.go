package main

import (
	"context"
	"fmt"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/poller"
	"github.com/ArjenSchwarz/flux/internal/poller/apns"
	"github.com/ArjenSchwarz/flux/internal/poller/eval"
)

// wireSocAlerts builds the SoC alert pipeline and attaches it to the poller.
// All construction is deferred until SocAlertsConfigured returns true, so a
// rollout that ships the binary before the CloudFormation update does not
// blow up — the alert path just stays inactive.
func wireSocAlerts(ctx context.Context, p *poller.Poller, cfg *config.Config, awsCfg awssdk.Config) error {
	ssmClient := ssm.NewFromConfig(awsCfg)
	creds, err := loadAPNsCredentials(ctx, ssmClient, cfg)
	if err != nil {
		return fmt.Errorf("load apns credentials: %w", err)
	}

	notifier, err := apns.NewNotifierFromCredentials(creds)
	if err != nil {
		return fmt.Errorf("build apns notifier: %w", err)
	}

	ddb := dynamodb.NewFromConfig(awsCfg)
	devices := dynamo.NewDynamoDeviceWriter(ddb, cfg.TableDevices)
	rules := dynamo.NewDynamoSocRuleReader(ddb, cfg.TableSocRules)
	fireState := dynamo.NewDynamoSocFireStateWriter(ddb, cfg.TableSocFireState)

	cache := eval.NewMemoizingRulesCache(
		&deviceLister{ddb: ddb, table: cfg.TableDevices, rules: rules},
		&perDeviceRulesAdapter{reader: rules},
	)
	queue := apns.NewQueue(apns.QueueConfig{
		Capacity: 64,
		Workers:  4,
		Notifier: notifier,
		Stale:    devices,
	})

	evalImpl := eval.NewEvaluator(cache, &fireStateAdapter{store: fireState}, &queueAdapter{q: queue})
	p.SetSocAlerts(evalImpl, queue)

	gcBackend := &orphanGCBackend{
		ddb:          ddb,
		devicesTable: cfg.TableDevices,
		devicesStore: devices,
		rulesReader:  rules,
		fireState:    fireState,
	}
	gc := poller.NewOrphanDeviceGC(gcBackend, 30*24*time.Hour, 24*time.Hour, time.Now)
	p.SetOrphanGC(gc)
	return nil
}

// loadAPNsCredentials fetches the five APNs SSM parameters in a single
// GetParameters call (one round-trip total, with decryption applied to the
// .p8 SecureString). All five must succeed; otherwise we fail loudly rather
// than partially initialising APNs.
func loadAPNsCredentials(ctx context.Context, client ssmAPI, cfg *config.Config) (apns.Credentials, error) {
	names := []string{
		cfg.APNsKeyParam,
		cfg.APNsKeyIDParam,
		cfg.APNsTeamIDParam,
		cfg.APNsBundleIDParam,
	}
	decrypt := true
	out, err := client.GetParameters(ctx, &ssm.GetParametersInput{
		Names:          names,
		WithDecryption: &decrypt,
	})
	if err != nil {
		return apns.Credentials{}, fmt.Errorf("get apns ssm params: %w", err)
	}
	if len(out.InvalidParameters) > 0 {
		return apns.Credentials{}, fmt.Errorf("apns ssm params missing: %v", out.InvalidParameters)
	}
	values := make(map[string]string, len(out.Parameters))
	for _, p := range out.Parameters {
		if p.Name != nil && p.Value != nil {
			values[*p.Name] = *p.Value
		}
	}
	return apns.Credentials{
		P8Key:    values[cfg.APNsKeyParam],
		KeyID:    values[cfg.APNsKeyIDParam],
		TeamID:   values[cfg.APNsTeamIDParam],
		BundleID: values[cfg.APNsBundleIDParam],
	}, nil
}

// ssmAPI is the subset of the SSM client used for parameter fetching.
type ssmAPI interface {
	GetParameters(ctx context.Context, params *ssm.GetParametersInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersOutput, error)
}

// fireStateAdapter bridges *dynamo.DynamoSocFireStateWriter to the eval
// package's FireStateRW interface. The eval interface uses an internal
// record type so the package boundary stays clean.
type fireStateAdapter struct {
	store *dynamo.DynamoSocFireStateWriter
}

func (a *fireStateAdapter) PutIfAbsent(ctx context.Context, rec eval.SoCFireStateRecord) (bool, error) {
	item := dynamo.NewSoCFireStateItem(
		rec.DeviceID, rec.RuleID, rec.WindowStartDate,
		rec.ObservedSoc, rec.APNsCollapseID, rec.FiredAt,
	)
	return a.store.PutIfAbsent(ctx, item)
}

// queueAdapter bridges *apns.Queue.Enqueue (which takes apns.Job) to
// eval's PushQueue interface (which takes eval.PushJob). Translation is
// trivial — both structs carry the same fields under different names.
type queueAdapter struct {
	q *apns.Queue
}

func (a *queueAdapter) Enqueue(ctx context.Context, job eval.PushJob) error {
	apnsJob := apns.Job{
		DeviceID:    job.DeviceID,
		RuleID:      job.RuleID,
		Token:       job.APNsToken,
		Environment: job.APNsEnvironment,
		CollapseID:  job.APNsCollapseID,
		Payload: apns.Payload{
			Title:            buildAlertTitle(job),
			Body:             buildAlertBody(job),
			RuleID:           job.RuleID,
			ThresholdPercent: job.ThresholdPercent,
			ObservedSoc:      job.ObservedSoc,
		},
	}
	if err := a.q.Enqueue(ctx, apnsJob); err != nil {
		// Translate to the eval package's ErrQueueFull so the evaluator
		// can recognise the overflow case without importing apns.
		return eval.ErrQueueFull
	}
	return nil
}

// buildAlertTitle / buildAlertBody compose the user-visible strings shown
// in the notification. The body includes the label when set.
func buildAlertTitle(job eval.PushJob) string {
	return fmt.Sprintf("Battery at %d%%", int(job.ObservedSoc))
}

func buildAlertBody(job eval.PushJob) string {
	if job.Label != "" {
		return fmt.Sprintf("%s — below %d%% until %s.", job.Label, job.ThresholdPercent, job.WindowEnd)
	}
	return fmt.Sprintf("Below %d%% until %s.", job.ThresholdPercent, job.WindowEnd)
}

// deviceLister adapts a DynamoDB Scan over flux-devices into the
// DeviceListReader interface eval expects. Per-device rules are fetched
// separately through perDeviceRulesAdapter.
type deviceLister struct {
	ddb   *dynamodb.Client
	table string
	rules *dynamo.DynamoSocRuleReader
}

func (l *deviceLister) ListDevices(ctx context.Context) ([]eval.DeviceWithRules, error) {
	items, err := dynamo.ScanDevices(ctx, l.ddb, l.table)
	if err != nil {
		return nil, err
	}
	out := make([]eval.DeviceWithRules, 0, len(items))
	for _, d := range items {
		out = append(out, eval.DeviceWithRules{
			DeviceID:        d.DeviceID,
			Platform:        d.Platform,
			APNsToken:       d.APNsToken,
			APNsEnvironment: d.APNsEnvironment,
			TZIdentifier:    d.TZIdentifier,
			TokenStatus:     d.TokenStatus,
		})
	}
	return out, nil
}

// orphanGCBackend adapts the dynamo stores into the
// poller.OrphanDeviceGCBackend interface. All read paths go through Scan or
// Query; the writes go through the existing conditional-delete helpers.
type orphanGCBackend struct {
	ddb          *dynamodb.Client
	devicesTable string
	devicesStore *dynamo.DynamoDeviceWriter
	rulesReader  *dynamo.DynamoSocRuleReader
	fireState    *dynamo.DynamoSocFireStateWriter
}

func (b *orphanGCBackend) ListDevices(ctx context.Context) ([]dynamo.DeviceItem, error) {
	return dynamo.ScanDevices(ctx, b.ddb, b.devicesTable)
}

func (b *orphanGCBackend) ListRulesByDevice(ctx context.Context, deviceID string) ([]dynamo.SoCRuleItem, error) {
	return b.rulesReader.ListRulesByDevice(ctx, deviceID)
}

func (b *orphanGCBackend) ListFireStateByDeviceRule(ctx context.Context, deviceID, ruleID string) ([]dynamo.SoCFireStateItem, error) {
	// Use the writer's Query path indirectly via DeleteByDeviceRule? No —
	// the GC needs the rows, not a side effect. The writer doesn't expose
	// a query method, but the deviceRule PK is well-known.
	return dynamo.QueryFireStateByDeviceRule(ctx, b.ddb, b.fireStateTableName(), deviceID, ruleID)
}

func (b *orphanGCBackend) fireStateTableName() string {
	// Pull the table name from the writer via an exported helper added
	// below. Kept local so callers don't reach into the writer struct.
	return b.fireState.Table()
}

func (b *orphanGCBackend) AnyFireStateNewerThan(ctx context.Context, deviceID string, cutoff time.Time) (bool, error) {
	rules, err := b.rulesReader.ListRulesByDevice(ctx, deviceID)
	if err != nil {
		return false, err
	}
	for _, r := range rules {
		rows, err := dynamo.QueryFireStateByDeviceRule(ctx, b.ddb, b.fireStateTableName(), deviceID, r.RuleID)
		if err != nil {
			return false, err
		}
		for _, fs := range rows {
			ft, err := time.Parse(time.RFC3339, fs.FiredAt)
			if err == nil && ft.After(cutoff) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (b *orphanGCBackend) DeleteFireStateRow(ctx context.Context, deviceRule, windowStartDate string) error {
	return dynamo.DeleteFireStateRow(ctx, b.ddb, b.fireStateTableName(), deviceRule, windowStartDate)
}

func (b *orphanGCBackend) DeleteRule(ctx context.Context, deviceID, ruleID string) error {
	// Reuse the writer used by the Lambda; the poller's IAM has DeleteItem
	// on flux-soc-rules (Decision 17 covers the Lambda; the poller has full
	// CRUD per design.md TaskRole policy).
	writer := dynamo.NewDynamoSocRuleWriter(b.ddb, b.rulesTable())
	return writer.DeleteRule(ctx, deviceID, ruleID)
}

func (b *orphanGCBackend) rulesTable() string {
	// The rules reader exposes its table name through a method added below.
	return b.rulesReader.Table()
}

func (b *orphanGCBackend) DeleteDeviceConditional(ctx context.Context, deviceID, scanned string) error {
	return b.devicesStore.DeleteDeviceConditional(ctx, deviceID, scanned)
}

// perDeviceRulesAdapter wraps DynamoSocRuleReader.ListRulesByDevice and
// projects DynamoDB rows into eval.RuleSnapshot.
type perDeviceRulesAdapter struct {
	reader *dynamo.DynamoSocRuleReader
}

func (a *perDeviceRulesAdapter) ListRulesForDevice(ctx context.Context, deviceID string) ([]eval.RuleSnapshot, error) {
	items, err := a.reader.ListRulesByDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	out := make([]eval.RuleSnapshot, 0, len(items))
	for _, r := range items {
		out = append(out, eval.RuleSnapshot{
			RuleID:           r.RuleID,
			ThresholdPercent: r.ThresholdPercent,
			WindowStart:      r.WindowStart,
			WindowEnd:        r.WindowEnd,
			Enabled:          r.Enabled,
			Label:            r.Label,
			UpdatedAt:        r.UpdatedAt,
			CreatedAt:        r.CreatedAt,
		})
	}
	return out, nil
}
