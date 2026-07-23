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

	// midnightFinalizerBuffer is the wait after Sydney midnight before
	// re-fetching yesterday's daily power and energy. Gives AlphaESS time to
	// publish the final 5-minute snapshot (23:55) and to settle the energy
	// totals out of the "all-zero finalisation" window.
	midnightFinalizerBuffer = 15 * time.Minute
)

// APIClient defines the AlphaESS API methods used by the poller.
type APIClient interface {
	GetLastPowerData(ctx context.Context, serial string) (*alphaess.PowerData, error)
	GetOneDayPower(ctx context.Context, serial, date string) ([]alphaess.PowerSnapshot, error)
	GetOneDateEnergy(ctx context.Context, serial, date string) (*alphaess.EnergyData, error)
	GetEssList(ctx context.Context, serial string) (*alphaess.SystemInfo, error)
}

// LiveDataEvaluator is the contract the live-data goroutine uses to fire
// SoC alerts. Defined here so the poller package does not import internal/poller/eval.
type LiveDataEvaluator interface {
	Evaluate(ctx context.Context, soc float64, readingAt time.Time)
}

// SocAlertsLifecycle is the lifecycle interface implemented by the push
// queue (Start/Stop). Optional — the poller calls these only when wired.
type SocAlertsLifecycle interface {
	Start()
	Stop(ctx context.Context)
}

// OrphanGC runs the daily device-orphan garbage collection step. Wired
// optionally so the integration tests don't need to construct it.
type OrphanGC interface {
	Run(ctx context.Context)
}

// Poller orchestrates multi-schedule polling of the AlphaESS API.
type Poller struct {
	client    APIClient
	store     dynamo.Store
	cfg       *config.Config
	plans     *PlanSource
	offpeak   *OffpeakScheduler
	metrics   MetricsRecorder
	evaluator LiveDataEvaluator
	queue     SocAlertsLifecycle
	orphanGC  OrphanGC

	// now returns the current time. Injectable for deterministic testing.
	now func() time.Time
}

// New creates a Poller with the given dependencies. The metrics recorder
// defaults to NoopMetrics; production code overwrites it via the SetMetrics
// helper after constructing a CloudWatch client.
//
// The off-peak scheduler and the summarisation pass share one PlanSource so
// they resolve the same window for a given day from the same cached read
// (Decision 2 — the plan is the single source of truth for the free window).
func New(client APIClient, store dynamo.Store, plans PlanLister, cfg *config.Config) *Poller {
	p := &Poller{
		client:  client,
		store:   store,
		cfg:     cfg,
		plans:   NewPlanSource(plans),
		now:     time.Now,
		metrics: NoopMetrics{},
	}
	p.offpeak = NewOffpeakScheduler(client, store, p.plans, cfg)
	return p
}

// SetMetrics overrides the metrics recorder. Used by cmd/poller to inject a
// real CloudWatch client; tests inject a fake. Safe to call before Run.
func (p *Poller) SetMetrics(m MetricsRecorder) {
	p.metrics = m
}

// SetSocAlerts wires the SoC alert evaluator and push queue. Both must be
// non-nil; the queue's lifecycle is managed by Run. Tests that don't exercise
// the SoC path simply don't call this and the live-poll path stays unchanged.
func (p *Poller) SetSocAlerts(eval LiveDataEvaluator, queue SocAlertsLifecycle) {
	p.evaluator = eval
	p.queue = queue
}

// SetOrphanGC wires the orphan-device garbage collector. Called by the
// midnight finalizer after yesterday's summarisation completes.
func (p *Poller) SetOrphanGC(gc OrphanGC) {
	p.orphanGC = gc
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

	if p.queue != nil {
		p.queue.Start()
	}

	var wg sync.WaitGroup
	wg.Add(7)
	go p.pollLiveData(ctx, drainCtx, &wg)
	go p.pollDailyPower(ctx, drainCtx, &wg)
	go p.pollDailyEnergy(ctx, drainCtx, &wg)
	go p.pollSystemInfo(ctx, drainCtx, &wg)
	go p.offpeak.Run(ctx, drainCtx, &wg)
	go p.pollDailySummary(ctx, drainCtx, &wg)
	go p.runMidnightFinalizer(ctx, drainCtx, &wg)

	<-ctx.Done()
	slog.Info("poller stopping")

	if p.queue != nil {
		p.queue.Stop(drainCtx)
	}

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

// pollDailyPower polls today and yesterday hourly. Yesterday is fetched so the
// final snapshots of the previous day (after the last pre-midnight tick) land
// in flux-daily-power; without this, Day Detail for past dates cuts off at the
// 5-minute boundary preceding the last pre-midnight poll.
func (p *Poller) pollDailyPower(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	pollLoop(loopCtx, drainCtx, wg, dailyPowerInterval, func(ctx context.Context) {
		p.fetchAndStoreDailyPower(ctx, "")
		yesterday := p.now().In(p.cfg.Location).AddDate(0, 0, -1).Format(dateLayout)
		p.fetchAndStoreDailyPower(ctx, yesterday)
	})
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

	// AlphaESS occasionally returns code:200 with present-but-all-zero values
	// when the inverter isn't actively reporting (observed overnight). Writing
	// that as a reading drives the iOS Dashboard to render 0% / 0 W as if
	// live. Skip the write and log the payload at warn so the gap is visible
	// in CloudWatch. (T-1274)
	if isAllZeroPower(data) {
		slog.Warn("skipping reading write: AlphaESS returned all-zero values (inverter likely not reporting)",
			"sysSn", p.cfg.Serial,
			"ppv", data.Ppv, "pload", data.Pload, "pbat", data.Pbat, "pgrid", data.Pgrid, "soc", data.Soc)
		return
	}

	item := dynamo.NewReadingItem(p.cfg.Serial, data, p.now())
	if err := p.store.WriteReading(ctx, item); err != nil {
		slog.Error("write reading failed", "error", err)
		return
	}
	slog.Info("stored reading", "sysSn", p.cfg.Serial)

	if p.evaluator != nil {
		p.evaluator.Evaluate(ctx, item.Soc, p.now())
	}
}

// isAllZeroPower reports whether every field on the live power response is
// zero. A working battery system never produces an all-zero live snapshot
// (SoC alone is always positive on a system that has been running), so such
// a response means AlphaESS isn't actually reporting current values.
func isAllZeroPower(d *alphaess.PowerData) bool {
	return d.Ppv == 0 && d.Pload == 0 && d.Pbat == 0 && d.Pgrid == 0 && d.Soc == 0
}

// fetchAndStoreDailyPower fetches and stores 5-minute power snapshots. If
// date is empty, uses today in the configured timezone.
func (p *Poller) fetchAndStoreDailyPower(ctx context.Context, date string) {
	if date == "" {
		date = p.now().In(p.cfg.Location).Format(dateLayout)
	}

	snapshots, err := p.client.GetOneDayPower(ctx, p.cfg.Serial, date)
	if err != nil {
		slog.Error("fetch daily power failed", "date", date, "error", err)
		return
	}

	if p.cfg.DryRun {
		logDryRunPayload("getOneDayPowerBySn", snapshots)
	}

	items := dynamo.NewDailyPowerItems(p.cfg.Serial, snapshots)
	if err := p.store.WriteDailyPower(ctx, items); err != nil {
		slog.Error("write daily power failed", "date", date, "error", err)
		return
	}
	slog.Info("stored daily power", "date", date, "count", len(items))
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

// runMidnightFinalizer waits until each Sydney midnight + midnightFinalizerBuffer,
// then re-fetches yesterday's daily power and daily energy and runs the
// derivedStats summarisation pass. Closes the gap left by the hourly tickers,
// whose phase relative to midnight depends on container start time and which
// could otherwise delay yesterday's finalisation by up to an hour.
// All operations here are idempotent — they overlap deliberately with the
// hourly pollDailyPower / pollDailyEnergy / pollDailySummary loops.
func (p *Poller) runMidnightFinalizer(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		target := nextLocalMidnight(p.now().In(p.cfg.Location)).Add(midnightFinalizerBuffer)
		delay := time.Until(target)
		if delay > 0 {
			select {
			case <-loopCtx.Done():
				return
			case <-time.After(delay):
			}
		}
		if loopCtx.Err() != nil {
			return
		}
		yesterday := p.now().In(p.cfg.Location).AddDate(0, 0, -1).Format(dateLayout)
		slog.Info("midnight finalizer running", "date", yesterday)
		p.fetchAndStoreDailyPower(drainCtx, yesterday)
		p.fetchAndStoreDailyEnergy(drainCtx, yesterday)
		p.runSummarisationPass(drainCtx, yesterday)
		if p.orphanGC != nil {
			p.orphanGC.Run(drainCtx)
		}
	}
}

// nextLocalMidnight returns 00:00:00 on the day following `now` in its
// timezone. Uses time.Date so it stays correct across DST transitions.
func nextLocalMidnight(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
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
