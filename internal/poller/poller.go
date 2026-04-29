package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

const (
	livePollInterval     = 10 * time.Second
	dailyPowerInterval   = 1 * time.Hour
	dailyEnergyInterval  = 1 * time.Hour
	dailySummaryInterval = 1 * time.Hour
	systemInfoInterval   = 24 * time.Hour
	shutdownDrainTimeout = 25 * time.Second
	dateLayout           = "2006-01-02"
)

// APIClient defines the AlphaESS API methods used by the poller.
type APIClient interface {
	GetLastPowerData(ctx context.Context, serial string) (*alphaess.PowerData, error)
	GetOneDayPower(ctx context.Context, serial, date string) ([]alphaess.PowerSnapshot, error)
	GetOneDateEnergy(ctx context.Context, serial, date string) (*alphaess.EnergyData, error)
	GetEssList(ctx context.Context, serial string) (*alphaess.SystemInfo, error)
}

// Poller orchestrates multi-schedule polling of the AlphaESS API.
type Poller struct {
	client  APIClient
	store   dynamo.Store
	cfg     *config.Config
	offpeak *OffpeakScheduler
	metrics MetricsRecorder

	// now returns the current time. Injectable for deterministic testing.
	now func() time.Time
}

// New creates a Poller with the given dependencies. The metrics recorder
// defaults to NoopMetrics; production code overwrites it via the SetMetrics
// helper after constructing a CloudWatch client.
func New(client APIClient, store dynamo.Store, cfg *config.Config) *Poller {
	p := &Poller{
		client:  client,
		store:   store,
		cfg:     cfg,
		now:     time.Now,
		metrics: NoopMetrics{},
	}
	p.offpeak = NewOffpeakScheduler(client, store, cfg)
	return p
}

// SetMetrics overrides the metrics recorder. Used by cmd/poller to inject a
// real CloudWatch client; tests inject a fake. Safe to call before Run.
func (p *Poller) SetMetrics(m MetricsRecorder) {
	p.metrics = m
}

// SetNow overrides the clock used by Run and the per-tick helpers. Intended
// for deterministic tests (notably the integration test, which lives in
// another package and cannot reach the unexported field). Safe to call
// before Run.
func (p *Poller) SetNow(now func() time.Time) {
	p.now = now
}

// SummariseYesterday runs one summarisation pass against the date that is
// "yesterday" in cfg.Location. Exposed for the integration test, which drives
// the pass without spinning up the full Run loop. Production code uses Run.
func (p *Poller) SummariseYesterday(ctx context.Context) {
	p.summariseYesterday(ctx)
}

// Run starts all polling goroutines and blocks until ctx is cancelled.
// Uses a two-context pattern: ctx (loopCtx) stops ticker loops, drainCtx
// allows in-flight operations up to 25s to complete.
func (p *Poller) Run(ctx context.Context) error {
	drainCtx, drainCancel := context.WithCancel(context.Background())
	defer drainCancel()

	var wg sync.WaitGroup
	wg.Add(6)
	go p.pollLiveData(ctx, drainCtx, &wg)
	go p.pollDailyPower(ctx, drainCtx, &wg)
	go p.pollDailyEnergy(ctx, drainCtx, &wg)
	go p.pollSystemInfo(ctx, drainCtx, &wg)
	go p.offpeak.Run(ctx, drainCtx, &wg)
	go p.pollDailySummary(ctx, drainCtx, &wg)

	<-ctx.Done()
	slog.Info("poller stopping")

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		return nil
	case <-time.After(shutdownDrainTimeout):
		drainCancel()
		return fmt.Errorf("shutdown timed out after %s", shutdownDrainTimeout)
	}
}

// pollLoop runs fn immediately, then on each tick until loopCtx is cancelled.
func pollLoop(loopCtx, drainCtx context.Context, wg *sync.WaitGroup, interval time.Duration, fn func(context.Context)) {
	defer wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	fn(drainCtx)

	for {
		select {
		case <-loopCtx.Done():
			return
		case <-ticker.C:
			fn(drainCtx)
		}
	}
}

func (p *Poller) pollLiveData(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	pollLoop(loopCtx, drainCtx, wg, livePollInterval, p.fetchAndStoreLiveData)
}

func (p *Poller) pollDailyPower(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	pollLoop(loopCtx, drainCtx, wg, dailyPowerInterval, p.fetchAndStoreDailyPower)
}

// pollDailyEnergy polls today and yesterday hourly; the zero-guard retries yesterday until AlphaESS finalises.
func (p *Poller) pollDailyEnergy(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	pollLoop(loopCtx, drainCtx, wg, dailyEnergyInterval, func(ctx context.Context) {
		p.fetchAndStoreDailyEnergy(ctx, "")
		yesterday := p.now().In(p.cfg.Location).AddDate(0, 0, -1).Format(dateLayout)
		p.fetchAndStoreDailyEnergy(ctx, yesterday)
	})
}

func (p *Poller) pollSystemInfo(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	pollLoop(loopCtx, drainCtx, wg, systemInfoInterval, p.fetchAndStoreSystemInfo)
}

// --- fetchAndStore helpers ---

func (p *Poller) fetchAndStoreLiveData(ctx context.Context) {
	data, err := p.client.GetLastPowerData(ctx, p.cfg.Serial)
	if err != nil {
		slog.Error("fetch live data failed", "error", err)
		return
	}

	if p.cfg.DryRun {
		logDryRunPayload("getLastPowerData", data)
	}

	item := dynamo.NewReadingItem(p.cfg.Serial, data, p.now())
	if err := p.store.WriteReading(ctx, item); err != nil {
		slog.Error("write reading failed", "error", err)
		return
	}
	slog.Info("stored reading", "sysSn", p.cfg.Serial)
}

func (p *Poller) fetchAndStoreDailyPower(ctx context.Context) {
	today := p.now().In(p.cfg.Location).Format(dateLayout)
	snapshots, err := p.client.GetOneDayPower(ctx, p.cfg.Serial, today)
	if err != nil {
		slog.Error("fetch daily power failed", "error", err)
		return
	}

	if p.cfg.DryRun {
		logDryRunPayload("getOneDayPowerBySn", snapshots)
	}

	items := dynamo.NewDailyPowerItems(p.cfg.Serial, snapshots, p.now())
	if err := p.store.WriteDailyPower(ctx, items); err != nil {
		slog.Error("write daily power failed", "error", err)
		return
	}
	slog.Info("stored daily power", "date", today, "count", len(items))
}

// fetchAndStoreDailyEnergy fetches and stores energy data. If date is empty,
// uses today in the configured timezone.
func (p *Poller) fetchAndStoreDailyEnergy(ctx context.Context, date string) {
	if date == "" {
		date = p.now().In(p.cfg.Location).Format(dateLayout)
	}

	data, err := p.client.GetOneDateEnergy(ctx, p.cfg.Serial, date)
	if err != nil {
		slog.Error("fetch daily energy failed", "date", date, "error", err)
		return
	}

	if p.cfg.DryRun {
		logDryRunPayload("getOneDateEnergyBySn", data)
	}

	// Defensive: current client can't return (nil, nil) but a future refactor shouldn't be able to panic here.
	if data == nil {
		slog.Warn("skipping daily energy write: nil response from AlphaESS", "date", date)
		return
	}
	// AlphaESS returns all-zero for yesterday during its finalisation window (extends past Sydney midnight).
	if isAllZeroEnergy(data) {
		slog.Warn("skipping daily energy write: AlphaESS returned all-zero (day not finalised yet)", "date", date)
		return
	}

	item := dynamo.NewDailyEnergyItem(p.cfg.Serial, date, data)
	if err := p.store.WriteDailyEnergy(ctx, item); err != nil {
		slog.Error("write daily energy failed", "date", date, "error", err)
		return
	}
	slog.Info("stored daily energy", "date", date)
}

func (p *Poller) fetchAndStoreSystemInfo(ctx context.Context) {
	info, err := p.client.GetEssList(ctx, p.cfg.Serial)
	if err != nil {
		slog.Error("fetch system info failed", "error", err)
		return
	}

	if p.cfg.DryRun {
		logDryRunPayload("getEssList", info)
	}

	item := dynamo.NewSystemItem(info, p.now())
	if err := p.store.WriteSystem(ctx, item); err != nil {
		slog.Error("write system info failed", "error", err)
		return
	}
	slog.Info("stored system info")
}

// isAllZeroEnergy reports whether every energy total in the AlphaESS response
// is zero. A working battery system never produces all-zero daily totals, so
// such a response means AlphaESS has not finalised the day's data yet.
func isAllZeroEnergy(d *alphaess.EnergyData) bool {
	return d.Epv == 0 && d.EInput == 0 && d.EOutput == 0 &&
		d.ECharge == 0 && d.EDischarge == 0 && d.EGridCharge == 0
}

// logDryRunPayload logs the raw API response payload at info level.
func logDryRunPayload(endpoint string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.Error("marshal dry-run payload", "endpoint", endpoint, "error", err)
		return
	}
	slog.Info("dry-run api response", "endpoint", endpoint, "payload", string(raw))
}
