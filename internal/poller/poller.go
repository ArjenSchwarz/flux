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
}

// New creates a Poller with the given dependencies.
func New(client APIClient, store dynamo.Store, cfg *config.Config) *Poller {
	p := &Poller{client: client, store: store, cfg: cfg}
	p.offpeak = NewOffpeakScheduler(client, store, cfg)
	return p
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
	go p.midnightFinalizer(ctx, drainCtx, &wg)

	<-ctx.Done()
	slog.Info("poller stopping")

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		return nil
	case <-time.After(25 * time.Second):
		drainCancel()
		return fmt.Errorf("shutdown timed out after 25 seconds")
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
	pollLoop(loopCtx, drainCtx, wg, 10*time.Second, p.fetchAndStoreLiveData)
}

func (p *Poller) pollDailyPower(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	pollLoop(loopCtx, drainCtx, wg, 1*time.Hour, p.fetchAndStoreDailyPower)
}

func (p *Poller) pollDailyEnergy(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	pollLoop(loopCtx, drainCtx, wg, 6*time.Hour, func(ctx context.Context) {
		p.fetchAndStoreDailyEnergy(ctx, "")
	})
}

func (p *Poller) pollSystemInfo(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	pollLoop(loopCtx, drainCtx, wg, 24*time.Hour, p.fetchAndStoreSystemInfo)
}

// midnightFinalizer waits until midnight+5min local time, then writes
// yesterday's final energy totals. Loops daily.
func (p *Poller) midnightFinalizer(loopCtx, drainCtx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		now := time.Now().In(p.cfg.Location)
		next := nextLocalMidnight(now, p.cfg.Location)
		delay := time.Until(next) + 5*time.Minute

		select {
		case <-loopCtx.Done():
			return
		case <-time.After(delay):
			yesterday := time.Now().In(p.cfg.Location).AddDate(0, 0, -1).Format("2006-01-02")
			slog.Debug("midnight finalizer running", "date", yesterday)
			p.fetchAndStoreDailyEnergy(drainCtx, yesterday)
		}
	}
}

// nextLocalMidnight returns the next midnight in the given location after now.
// Uses time.Date for DST safety.
func nextLocalMidnight(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc)
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

	now := time.Now()
	item := dynamo.NewReadingItem(p.cfg.Serial, data, now)
	if err := p.store.WriteReading(ctx, item); err != nil {
		slog.Error("write reading failed", "error", err)
	}
}

func (p *Poller) fetchAndStoreDailyPower(ctx context.Context) {
	today := time.Now().In(p.cfg.Location).Format("2006-01-02")
	snapshots, err := p.client.GetOneDayPower(ctx, p.cfg.Serial, today)
	if err != nil {
		slog.Error("fetch daily power failed", "error", err)
		return
	}

	if p.cfg.DryRun {
		logDryRunPayload("getOneDayPowerBySn", snapshots)
	}

	now := time.Now()
	items := dynamo.NewDailyPowerItems(p.cfg.Serial, snapshots, now)
	if err := p.store.WriteDailyPower(ctx, items); err != nil {
		slog.Error("write daily power failed", "error", err)
	}
}

// fetchAndStoreDailyEnergy fetches and stores energy data. If date is empty,
// uses today in the configured timezone.
func (p *Poller) fetchAndStoreDailyEnergy(ctx context.Context, date string) {
	if date == "" {
		date = time.Now().In(p.cfg.Location).Format("2006-01-02")
	}

	data, err := p.client.GetOneDateEnergy(ctx, p.cfg.Serial, date)
	if err != nil {
		slog.Error("fetch daily energy failed", "date", date, "error", err)
		return
	}

	if p.cfg.DryRun {
		logDryRunPayload("getOneDateEnergyBySn", data)
	}

	item := dynamo.NewDailyEnergyItem(p.cfg.Serial, date, data)
	if err := p.store.WriteDailyEnergy(ctx, item); err != nil {
		slog.Error("write daily energy failed", "date", date, "error", err)
	}
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

	now := time.Now()
	item := dynamo.NewSystemItem(info, now)
	if err := p.store.WriteSystem(ctx, item); err != nil {
		slog.Error("write system info failed", "error", err)
	}
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
