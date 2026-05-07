package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// KnowledgePostProcessService acts as an orchestrator for all post-processing tasks
// after a document has been parsed and split into chunks (including multimodal OCR/Caption).
type KnowledgePostProcessService struct {
	knowledgeRepo interfaces.KnowledgeRepository
	kbService     interfaces.KnowledgeBaseService
	chunkService  interfaces.ChunkService
	taskEnqueuer  interfaces.TaskEnqueuer
	redisClient   *redis.Client
}

func NewKnowledgePostProcessService(
	knowledgeRepo interfaces.KnowledgeRepository,
	kbService interfaces.KnowledgeBaseService,
	chunkService interfaces.ChunkService,
	taskEnqueuer interfaces.TaskEnqueuer,
	redisClient *redis.Client,
) interfaces.TaskHandler {
	return &KnowledgePostProcessService{
		knowledgeRepo: knowledgeRepo,
		kbService:     kbService,
		chunkService:  chunkService,
		taskEnqueuer:  taskEnqueuer,
		redisClient:   redisClient,
	}
}

// Handle implements asynq handler for TypeKnowledgePostProcess.
func (s *KnowledgePostProcessService) Handle(ctx context.Context, task *asynq.Task) error {
	var payload types.KnowledgePostProcessPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal knowledge post process payload: %w", err)
	}

	logger.Infof(ctx, "[KnowledgePostProcess] Orchestrating post processing for knowledge: %s", payload.KnowledgeID)

	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	if payload.Language != "" {
		ctx = context.WithValue(ctx, types.LanguageContextKey, payload.Language)
	}

	// 1. Fetch Knowledge and KB
	knowledge, err := s.knowledgeRepo.GetKnowledgeByIDOnly(ctx, payload.KnowledgeID)
	if err != nil {
		return fmt.Errorf("get knowledge %s: %w", payload.KnowledgeID, err)
	}
	if knowledge == nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Knowledge %s not found, aborting.", payload.KnowledgeID)
		return nil
	}

	// Heartbeat updated_at while we're still on parse_status='processing'.
	// processChunks ran the heartbeat during indexing, then exited; this
	// asynq task picks it back up so the watchdog stays accurate during
	// any work this handler does before it flips status to 'completed'
	// (e.g. listing chunks for a 50k-chunk document is non-trivial).
	if knowledge.ParseStatus == types.ParseStatusProcessing {
		stopHeartbeat := startKnowledgeHeartbeat(ctx, s.knowledgeRepo, payload.KnowledgeID)
		defer stopHeartbeat()
	}

	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, payload.KnowledgeBaseID)
	if err != nil || kb == nil {
		return fmt.Errorf("get knowledge base %s: %w", payload.KnowledgeBaseID, err)
	}

	// 2. Fetch all chunks
	chunks, err := s.chunkService.ListChunksByKnowledgeID(ctx, payload.KnowledgeID)
	if err != nil {
		return fmt.Errorf("list chunks for knowledge %s: %w", payload.KnowledgeID, err)
	}

	// Gather all text-like chunks (including newly added OCR and Caption from multimodal tasks)
	var textChunks []*types.Chunk
	for _, c := range chunks {
		if c.ChunkType == types.ChunkTypeText || c.ChunkType == types.ChunkTypeImageOCR || c.ChunkType == types.ChunkTypeImageCaption {
			textChunks = append(textChunks, c)
		}
	}

	// 3. Update ParseStatus to Completed
	// (Except if it's already completed or if it was marked as failed/deleting, but we'll just set it to completed if it's processing)
	if knowledge.ParseStatus == types.ParseStatusProcessing {
		knowledge.ParseStatus = types.ParseStatusCompleted
		knowledge.UpdatedAt = time.Now()

		// Setup summary status
		if len(textChunks) > 0 {
			knowledge.SummaryStatus = types.SummaryStatusPending
		} else {
			knowledge.SummaryStatus = types.SummaryStatusNone
		}

		if err := s.knowledgeRepo.UpdateKnowledge(ctx, knowledge); err != nil {
			logger.Warnf(ctx, "[KnowledgePostProcess] Failed to update knowledge status to completed: %v", err)
		} else {
			logger.Infof(ctx, "[KnowledgePostProcess] Knowledge %s marked as completed.", payload.KnowledgeID)
		}
	}

	// 4. Spawn Summary and Question Tasks
	if len(textChunks) > 0 {
		s.enqueueSummaryGenerationTask(ctx, payload)
		// Question generation only makes sense for RAG indexing (improves chunk recall).
		// Skip when only Wiki/Graph is enabled without vector/keyword search.
		if kb.NeedsEmbeddingModel() {
			s.enqueueQuestionGenerationIfEnabled(ctx, payload, kb)
		}
	}

	// 5. Spawn Graph RAG Tasks — only when graph indexing is enabled in IndexingStrategy
	if kb.IsGraphEnabled() {
		logger.Infof(ctx, "[KnowledgePostProcess] Spawning Graph RAG extract tasks for %d text-like chunks", len(textChunks))
		for _, chunk := range textChunks {
			err := NewChunkExtractTask(ctx, s.taskEnqueuer, payload.TenantID, chunk.ID, kb.SummaryModelID)
			if err != nil {
				logger.Errorf(ctx, "[KnowledgePostProcess] Failed to create chunk extract task for %s: %v", chunk.ID, err)
			}
		}
	}

	// 6. Spawn Wiki Ingest Task if wiki indexing is enabled in IndexingStrategy
	if kb.IndexingStrategy.WikiEnabled && len(textChunks) > 0 {
		EnqueueWikiIngest(ctx, s.taskEnqueuer, s.redisClient, payload.TenantID, payload.KnowledgeBaseID, payload.KnowledgeID)
		logger.Infof(ctx, "[KnowledgePostProcess] Enqueued wiki ingest task for %s", payload.KnowledgeID)
	}
	return nil
}

func (s *KnowledgePostProcessService) enqueueSummaryGenerationTask(ctx context.Context, payload types.KnowledgePostProcessPayload) {
	if s.taskEnqueuer == nil {
		return
	}

	taskPayload := types.SummaryGenerationPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
		KnowledgeID:     payload.KnowledgeID,
		Language:        payload.Language,
	}
	langfuse.InjectTracing(ctx, &taskPayload)
	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to marshal summary generation payload: %v", err)
		return
	}

	// 30 min matches Asynq's previous implicit default — slow self-hosted
	// LLMs and rate-limited APIs routinely sit between 15 and 30 min, so a
	// 15-minute cap would surface as a regression for those tenants. The
	// summaryOpts() helper applies that default; tunable via
	// WEKNORA_SUMMARY_TIMEOUT. The deterministic TaskID lets the
	// reconciler look up the running task to distinguish "live" from
	// "orphaned in processing" — without it the reconciler would
	// false-positive an active summary as a dead orphan.
	task := asynq.NewTask(types.TypeSummaryGeneration, payloadBytes,
		summaryOpts(asynq.TaskID(SummaryTaskID(payload.KnowledgeID)))...)
	if _, err := s.taskEnqueuer.Enqueue(task); err != nil {
		if IsTaskIDConflict(err) {
			logger.Infof(ctx, "[KnowledgePostProcess] Summary task already queued for %s, skipping", payload.KnowledgeID)
		} else {
			logger.Warnf(ctx, "[KnowledgePostProcess] Failed to enqueue summary generation for %s: %v", payload.KnowledgeID, err)
		}
	} else {
		logger.Infof(ctx, "[KnowledgePostProcess] Enqueued summary generation task for %s", payload.KnowledgeID)
	}
}

func (s *KnowledgePostProcessService) enqueueQuestionGenerationIfEnabled(ctx context.Context, payload types.KnowledgePostProcessPayload, kb *types.KnowledgeBase) {
	if s.taskEnqueuer == nil {
		return
	}

	if kb.QuestionGenerationConfig == nil || !kb.QuestionGenerationConfig.Enabled {
		return
	}

	questionCount := kb.QuestionGenerationConfig.QuestionCount
	if questionCount <= 0 {
		questionCount = 3
	}
	if questionCount > 10 {
		questionCount = 10
	}

	taskPayload := types.QuestionGenerationPayload{
		TenantID:        payload.TenantID,
		KnowledgeBaseID: payload.KnowledgeBaseID,
		KnowledgeID:     payload.KnowledgeID,
		QuestionCount:   questionCount,
		Language:        payload.Language,
	}
	langfuse.InjectTracing(ctx, &taskPayload)
	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Warnf(ctx, "[KnowledgePostProcess] Failed to marshal question generation payload: %v", err)
		return
	}

	// 30 min: Q-Gen is N serial LLM calls (one per question slot up to
	// QuestionCount). On slow LLMs that easily exceeds 15 min for a single
	// knowledge with QuestionCount=10. Match Asynq's previous default to
	// avoid silently regressing those tenants. The qgOpts() helper applies
	// that default; tunable via WEKNORA_QG_TIMEOUT. The deterministic
	// TaskID is what lets the reconciler tell a running QG apart from a
	// genuinely orphaned row, and what lets ReparseKnowledge cancel a
	// stale QG before re-enqueueing.
	task := asynq.NewTask(types.TypeQuestionGeneration, payloadBytes,
		qgOpts(asynq.TaskID(QGTaskID(payload.KnowledgeID)))...)
	if _, err := s.taskEnqueuer.Enqueue(task); err != nil {
		if IsTaskIDConflict(err) {
			logger.Infof(ctx, "[KnowledgePostProcess] QG task already queued for %s, skipping", payload.KnowledgeID)
		} else {
			logger.Warnf(ctx, "[KnowledgePostProcess] Failed to enqueue question generation for %s: %v", payload.KnowledgeID, err)
		}
	} else {
		logger.Infof(ctx, "[KnowledgePostProcess] Enqueued question generation task for %s (count=%d)", payload.KnowledgeID, questionCount)
	}
}
