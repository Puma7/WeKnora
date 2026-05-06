package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

// KnowledgeReconciler periodically scans the database for Knowledge rows
// that have been in "processing" state longer than WEKNORA_STUCK_THRESHOLD
// without a corresponding live asynq task. Such rows are orphans —
// either the worker died before lease renewal, the asynq broker lost
// the task, or all retries were exhausted and the task was archived.
//
// In all of these cases the row would otherwise stay "processing"
// forever, blocking the user. The reconciler restores liveness by
// either marking the row as failed (default, safe) or re-enqueueing
// (opt-in, when WEKNORA_RECONCILE_ARCHIVED_ACTION=requeue).
//
// We deliberately do NOT have the reconciler re-run the original
// enqueue path: that would require resurrecting the original payload
// (KB config snapshot at upload time), which the DB doesn't preserve.
// Instead, requeue mode delegates to ReparseKnowledge, which rebuilds
// the payload from current KB config — same as a manual reparse.
type KnowledgeReconciler struct {
	repo             interfaces.KnowledgeRepository
	knowledgeService interfaces.KnowledgeService
	redisOpt         asynq.RedisConnOpt
}

func NewKnowledgeReconciler(
	repo interfaces.KnowledgeRepository,
	knowledgeService interfaces.KnowledgeService,
	redisOpt asynq.RedisConnOpt,
) *KnowledgeReconciler {
	return &KnowledgeReconciler{
		repo:             repo,
		knowledgeService: knowledgeService,
		redisOpt:         redisOpt,
	}
}

// Run loops until ctx is done. Errors are logged and the loop
// continues — never crash the process on a reconcile failure.
func (r *KnowledgeReconciler) Run(ctx context.Context) {
	if r.redisOpt == nil {
		// Lite mode (no Redis) is already handled by resetPendingTasks at
		// startup; reconciler is a Redis-only feature.
		logger.Info(ctx, "[Reconciler] no Redis configured, reconciler disabled")
		return
	}
	interval := envDurationDefault("WEKNORA_RECONCILE_INTERVAL", 5*time.Minute)
	logger.Infof(ctx, "[Reconciler] starting; interval=%s threshold=%s archived_action=%s",
		interval, reconcileStuckThreshold(), reconcileArchivedAction())

	// Run once at startup so a fresh-start finds prior orphans without
	// waiting for the first tick.
	if err := r.cycle(ctx); err != nil {
		logger.Warnf(ctx, "[Reconciler] initial cycle error: %v", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info(ctx, "[Reconciler] stopping")
			return
		case <-ticker.C:
			if err := r.cycle(ctx); err != nil {
				logger.Warnf(ctx, "[Reconciler] cycle error: %v", err)
			}
		}
	}
}

func reconcileStuckThreshold() time.Duration {
	return envDurationDefault("WEKNORA_STUCK_THRESHOLD", 2*time.Hour)
}

func reconcileArchivedAction() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("WEKNORA_RECONCILE_ARCHIVED_ACTION")))
	if v == "requeue" {
		return "requeue"
	}
	return "fail"
}

// cycle scans for stuck rows and dispatches them.
func (r *KnowledgeReconciler) cycle(ctx context.Context) error {
	threshold := time.Now().Add(-reconcileStuckThreshold())

	inspector := asynq.NewInspector(r.redisOpt)
	defer inspector.Close()

	// Phase 1: stamp pre-migration / not-yet-observed rows so they enter
	// the stuck scan after a SHORTER grace period (default 15min) than
	// the normal stuck threshold (default 2h). The stamp is back-dated
	// by (stuckThreshold - gracePeriod) so the row appears "almost
	// stuck" — a real worker heartbeat will refresh it well before the
	// next cycle and remove it from the scan; a genuinely orphaned row
	// will get classified within ~gracePeriod instead of waiting the
	// full threshold.
	gracePeriod := envDurationDefault("WEKNORA_RECONCILE_GRACE_PERIOD", 15*time.Minute)
	stuckThreshold := reconcileStuckThreshold()
	backdate := time.Now().Add(-(stuckThreshold - gracePeriod))
	if backdate.After(time.Now()) {
		backdate = time.Now()
	}
	unobserved, err := r.repo.ListUnobservedKnowledge(ctx, 1000)
	if err != nil {
		logger.Warnf(ctx, "[Reconciler] list unobserved: %v", err)
	}
	ids := make([]string, 0, len(unobserved))
	for _, k := range unobserved {
		ids = append(ids, k.ID)
	}
	stamped, err := r.repo.BulkStampProcessingStartedAt(ctx, ids, backdate)
	if err != nil {
		logger.Warnf(ctx, "[Reconciler] bulk grace-stamp: %v", err)
	}

	// Phase 2: scan rows whose heartbeat IS set and older than threshold.
	parseRows, err := r.repo.ListStuckKnowledge(ctx, threshold, 200)
	if err != nil {
		return fmt.Errorf("list stuck parsing: %w", err)
	}

	var live, requeued, failedCount, errCount int
	for _, k := range parseRows {
		action, err := r.reconcileParse(ctx, inspector, k)
		if err != nil {
			errCount++
			logger.Warnf(ctx, "[Reconciler] parse reconcile %s: %v", k.ID, err)
			continue
		}
		switch action {
		case "live":
			live++
		case "requeued":
			requeued++
		case "failed":
			failedCount++
		}
	}

	if len(parseRows) > 0 || requeued > 0 || failedCount > 0 || stamped > 0 {
		logger.Infof(ctx,
			"[Reconciler] cycle: grace_stamped=%d parse(stuck=%d live=%d requeued=%d failed=%d errors=%d)",
			stamped, len(parseRows), live, requeued, failedCount, errCount,
		)
	}
	return nil
}

// reconcileParse classifies a stuck-in-processing Knowledge by checking
// whether its deterministic asynq task is still alive in some queue.
// Returns one of: "live", "requeued", "failed".
func (r *KnowledgeReconciler) reconcileParse(
	ctx context.Context, inspector *asynq.Inspector, k *types.Knowledge,
) (string, error) {
	// Try both task IDs — manual knowledge uses ManualProcessTaskID, file
	// knowledge uses DocProcessTaskID. We don't know which the row used.
	taskIDs := []string{DocProcessTaskID(k.ID), ManualProcessTaskID(k.ID)}
	for _, taskID := range taskIDs {
		state, err := r.lookupTaskState(inspector, "default", taskID)
		if err != nil {
			return "", err
		}
		switch state {
		case "active", "pending", "scheduled", "retry":
			// Worker still has it. The row's heartbeat is just stale —
			// will be refreshed on the next chunk-batch update.
			return "live", nil
		case "archived":
			return r.handleArchived(ctx, k, inspector, taskID)
		}
	}
	// No task in any queue with either ID. Treat as orphan.
	return r.handleOrphan(ctx, k)
}

func (r *KnowledgeReconciler) handleArchived(
	ctx context.Context, k *types.Knowledge, inspector *asynq.Inspector, taskID string,
) (string, error) {
	if reconcileArchivedAction() == "requeue" {
		// asynq's RunTask moves an archived task back to pending. This
		// preserves the original payload, which is the cleanest way to
		// retry — no need to reconstruct.
		if err := inspector.RunTask("default", taskID); err != nil {
			return "", fmt.Errorf("run archived task: %w", err)
		}
		// Reset processing_started_at so we don't immediately re-flag it.
		if err := r.repo.TouchProcessingHeartbeat(ctx, k.ID); err != nil {
			logger.Warnf(ctx, "[Reconciler] heartbeat reset after requeue %s: %v", k.ID, err)
		}
		return "requeued", nil
	}
	if err := r.failKnowledge(ctx, k, "asynq task archived after retry exhaustion"); err != nil {
		return "", err
	}
	return "failed", nil
}

func (r *KnowledgeReconciler) handleOrphan(ctx context.Context, k *types.Knowledge) (string, error) {
	// Orphan means no task exists for this Knowledge in any queue, but
	// the row is still in "processing". Default action is to mark it
	// failed so the user can decide. With requeue mode, we delegate to
	// ReparseKnowledge which rebuilds the payload from current KB config.
	if reconcileArchivedAction() == "requeue" && r.knowledgeService != nil {
		// ReparseKnowledge expects tenant context.
		reparseCtx := context.WithValue(ctx, types.TenantIDContextKey, k.TenantID)
		if _, err := r.knowledgeService.ReparseKnowledge(reparseCtx, k.ID); err != nil {
			return "", fmt.Errorf("reparse: %w", err)
		}
		return "requeued", nil
	}
	if err := r.failKnowledge(ctx, k, "no live task and no archived task — likely orphaned by crash"); err != nil {
		return "", err
	}
	return "failed", nil
}

func (r *KnowledgeReconciler) failKnowledge(ctx context.Context, k *types.Knowledge, msg string) error {
	k.ParseStatus = types.ParseStatusFailed
	if strings.TrimSpace(k.ErrorMessage) == "" {
		k.ErrorMessage = msg
	}
	k.UpdatedAt = time.Now()
	return r.repo.UpdateKnowledge(ctx, k)
}

func (r *KnowledgeReconciler) lookupTaskState(
	inspector *asynq.Inspector, queue, taskID string,
) (string, error) {
	info, err := inspector.GetTaskInfo(queue, taskID)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskNotFound) || errors.Is(err, asynq.ErrQueueNotFound) {
			return "missing", nil
		}
		return "", err
	}
	switch info.State {
	case asynq.TaskStateActive:
		return "active", nil
	case asynq.TaskStatePending:
		return "pending", nil
	case asynq.TaskStateScheduled:
		return "scheduled", nil
	case asynq.TaskStateRetry:
		return "retry", nil
	case asynq.TaskStateArchived:
		return "archived", nil
	case asynq.TaskStateCompleted:
		return "completed", nil
	}
	return "missing", nil
}
