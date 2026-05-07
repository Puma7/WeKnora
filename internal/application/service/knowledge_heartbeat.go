package service

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// heartbeatInterval is the cadence at which a long-running ingest writes
// updated_at on the knowledges row to signal liveness. Watchdog threshold
// (5 min, see knowledge_watchdog.go) gives ~5 ticks of headroom.
const heartbeatInterval = 60 * time.Second

// heartbeatLogEvery throttles failure logs so a multi-minute DB outage
// across many parallel ingests does not generate one warn line per
// failed write per knowledge per minute.
const heartbeatLogEvery = 10

// startKnowledgeHeartbeat pings knowledge.updated_at every heartbeatInterval
// so the stuck-task watchdog can tell active long-running work apart from
// wedged workers. The first tick fires immediately so very fast-completing
// pipelines still leave a single liveness signal, and so the watchdog has
// no T<heartbeatInterval blind window after a parse_status='processing'
// transition.
//
// The returned cancel func MUST be called (typically via defer) to stop the
// goroutine. The heartbeat runs on a detached context.Background so that
// request-side cancellation does not race the final terminal-status write
// that the caller is about to perform.
//
// Failure-log rate-limit: only every heartbeatLogEvery-th DB error gets
// logged (counted across the lifetime of this heartbeat). Operators still
// see the first failure plus periodic reminders without log floods.
func startKnowledgeHeartbeat(
	ctx context.Context,
	repo interfaces.KnowledgeRepository,
	knowledgeID string,
) func() {
	hbCtx, cancel := context.WithCancel(context.Background())
	go func() {
		var failures uint64
		writeOnce := func() {
			if err := repo.UpdateKnowledgeColumn(hbCtx, knowledgeID, "updated_at", time.Now()); err != nil {
				n := atomic.AddUint64(&failures, 1)
				if n == 1 || n%heartbeatLogEvery == 0 {
					logger.Warnf(ctx,
						"heartbeat updated_at write failed for %s (failure #%d): %v",
						knowledgeID, n, err)
				}
			}
		}

		// Immediate first write so the pipeline does not have a 60s blind
		// window where parse_status='processing' but updated_at hasn't been
		// touched yet by the heartbeat itself.
		writeOnce()

		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				writeOnce()
			}
		}
	}()
	return cancel
}
