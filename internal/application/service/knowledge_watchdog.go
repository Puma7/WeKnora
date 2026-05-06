// Package service contains application-layer services. KnowledgeWatchdog
// is a log-only observability cron that surfaces ingest tasks wedged in
// parse_status='processing' without recent updated_at progress. It does
// not mutate state — operators decide whether to retry, fail, or escalate.
package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/robfig/cron/v3"
)

// KnowledgeWatchdog periodically scans for indexing tasks that have been
// stuck in 'processing' without a heartbeat. It only writes warn-level
// logs — no status transitions, no re-enqueue. The threshold is paired
// with the 60s heartbeat in knowledge_process.go::startProcessingHeartbeat
// (a 5-minute threshold gives ~5 ticks of headroom before a stall is
// surfaced).
type KnowledgeWatchdog struct {
	repo      interfaces.KnowledgeRepository
	cron      *cron.Cron
	threshold time.Duration
	limit     int
}

// NewKnowledgeWatchdog wires the watchdog with sensible defaults. The
// scan threshold (5 min) and per-tick row limit (100) are intentionally
// not exposed via config — they are sized against the heartbeat interval
// and the practical "what's worth a single warn line" cutoff.
func NewKnowledgeWatchdog(repo interfaces.KnowledgeRepository) *KnowledgeWatchdog {
	return &KnowledgeWatchdog{
		repo: repo,
		cron: cron.New(cron.WithChain(
			cron.Recover(cron.DefaultLogger),
		)),
		threshold: 5 * time.Minute,
		limit:     100,
	}
}

// Start registers the periodic scan and kicks the cron runner. Errors
// are limited to schedule-parse failures; the scan itself never propagates
// errors out of the goroutine — they are logged and the next tick retries.
func (w *KnowledgeWatchdog) Start(ctx context.Context) error {
	_, err := w.cron.AddFunc("@every 5m", func() {
		w.scan(context.Background())
	})
	if err != nil {
		return err
	}
	w.cron.Start()
	logger.Infof(ctx, "[KnowledgeWatchdog] started (threshold=%s, limit=%d)", w.threshold, w.limit)
	return nil
}

// Stop gracefully stops the cron runner.
func (w *KnowledgeWatchdog) Stop() {
	stopCtx := w.cron.Stop()
	<-stopCtx.Done()
}

// scan runs one detection pass. Pure side effect: structured warn logs.
func (w *KnowledgeWatchdog) scan(ctx context.Context) {
	stuck, err := w.repo.FindStuckProcessing(ctx, w.threshold, w.limit)
	if err != nil {
		logger.Warnf(ctx, "[KnowledgeWatchdog] scan failed: %v", err)
		return
	}
	if len(stuck) == 0 {
		return
	}
	for _, k := range stuck {
		logger.Warnf(ctx,
			"[KnowledgeWatchdog] stuck processing task detected: knowledge_id=%s tenant_id=%d kb_id=%s file_name=%q updated_at=%s threshold=%s",
			k.ID, k.TenantID, k.KnowledgeBaseID, k.FileName, k.UpdatedAt.Format(time.RFC3339), w.threshold,
		)
	}
}
