package router

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"go.uber.org/dig"
)

type AsynqTaskParams struct {
	dig.In

	Server               *asynq.Server
	KnowledgeService     interfaces.KnowledgeService
	KnowledgeBaseService interfaces.KnowledgeBaseService
	TagService           interfaces.KnowledgeTagService
	DataSourceService    interfaces.DataSourceService
	ChunkExtractor       interfaces.TaskHandler `name:"chunkExtractor"`
	DataTableSummary     interfaces.TaskHandler `name:"dataTableSummary"`
	ImageMultimodal      interfaces.TaskHandler `name:"imageMultimodal"`
	KnowledgePostProcess interfaces.TaskHandler `name:"knowledgePostProcess"`
	WikiIngest           interfaces.TaskHandler `name:"wikiIngest"`
	Reconciler           *service.KnowledgeReconciler
}

// AsynqRedisConnOpt exposes the Redis connection options as the
// asynq.RedisConnOpt interface so other components (notably the
// KnowledgeReconciler's *asynq.Inspector) can connect to the same
// broker without re-reading env vars.
func AsynqRedisConnOpt() asynq.RedisConnOpt {
	return getAsynqRedisClientOpt()
}

// envIntDefault reads an integer from an env var, falling back to def when
// unset, empty, or unparseable. Negative values also fall back to keep
// callers from accidentally configuring poison values.
func envIntDefault(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	parsed, err := strconv.Atoi(v)
	if err != nil || parsed < 0 {
		return def
	}
	return parsed
}

func getAsynqRedisClientOpt() *asynq.RedisClientOpt {
	db := 0
	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		if parsed, err := strconv.Atoi(dbStr); err == nil {
			db = parsed
		}
	}
	// Default timeouts were 100ms read / 200ms write — too tight under load:
	// a brief Redis stall (GC pause, network hiccup, large pending list scan)
	// surfaces as spurious task fetch failures and asynq heartbeat misses.
	// 3s gives ample headroom while staying well below any reasonable task
	// timeout. Override via WEKNORA_REDIS_*_TIMEOUT_MS if needed.
	readTimeoutMs := envIntDefault("WEKNORA_REDIS_READ_TIMEOUT_MS", 3000)
	writeTimeoutMs := envIntDefault("WEKNORA_REDIS_WRITE_TIMEOUT_MS", 3000)
	opt := &asynq.RedisClientOpt{
		Addr:         os.Getenv("REDIS_ADDR"),
		Username:     os.Getenv("REDIS_USERNAME"),
		Password:     os.Getenv("REDIS_PASSWORD"),
		ReadTimeout:  time.Duration(readTimeoutMs) * time.Millisecond,
		WriteTimeout: time.Duration(writeTimeoutMs) * time.Millisecond,
		DB:           db,
	}
	return opt
}

func NewAsyncqClient() (*asynq.Client, error) {
	opt := getAsynqRedisClientOpt()
	client := asynq.NewClient(opt)
	err := client.Ping()
	if err != nil {
		return nil, err
	}
	return client, nil
}

// wikiIngestRetryDelay is a fixed, short backoff for wiki ingest lock
// conflicts. Must be slightly longer than the active-lock TTL's worst-case
// "just got set" window so the retry is highly likely to succeed without
// burning through retries; but short enough that users don't feel the stall.
const wikiIngestRetryDelay = 15 * time.Second

// asynqRetryDelayFunc customizes per-task retry backoff.
//
// Default asynq backoff is exponential (≈10s, 40s, 90s, 2.5m, ...), which
// is appropriate for transient errors like remote HTTP failures. But for
// wiki ingest lock conflicts (ErrWikiIngestConcurrent), exponential
// backoff is harmful: a freshly orphaned lock expires in ≤60s, so a 15s
// fixed retry virtually guarantees the next attempt succeeds. Without
// this override, a crash-restart cycle can leave a KB unable to make
// progress for 7–10 minutes while the orphan lock expires AND the retry
// schedule catches up.
func asynqRetryDelayFunc(n int, e error, t *asynq.Task) time.Duration {
	if errors.Is(e, service.ErrWikiIngestConcurrent) {
		return wikiIngestRetryDelay
	}
	return asynq.DefaultRetryDelayFunc(n, e, t)
}

func NewAsynqServer() *asynq.Server {
	opt := getAsynqRedisClientOpt()
	// Concurrency = total number of worker goroutines across all queues;
	// the priority weights below only govern dispatch ratio. asynq's own
	// default (when Concurrency=0) is runtime.NumCPU() — see asynq
	// server.go:451. We pass 0 by default so high-core hosts use all
	// available cores. Set WEKNORA_ASYNQ_CONCURRENCY=N to pin to N.
	concurrency := envIntDefault("WEKNORA_ASYNQ_CONCURRENCY", 0)
	srv := asynq.NewServer(
		opt,
		asynq.Config{
			Concurrency: concurrency,
			Queues: map[string]int{
				"critical": 6, // Highest priority queue
				"default":  3, // Default priority queue
				"low":      1, // Lowest priority queue
			},
			RetryDelayFunc: asynqRetryDelayFunc,
		},
	)
	return srv
}

func RunAsynqServer(params AsynqTaskParams) *asynq.ServeMux {
	// Create a new mux and register all handlers
	mux := asynq.NewServeMux()

	// Install Langfuse middleware BEFORE handler registration so every task
	// type is automatically wrapped. When Langfuse is disabled the middleware
	// is a pass-through; when enabled it resumes the upstream HTTP trace (if
	// the payload carries one) or opens a standalone trace, then wraps the
	// handler execution in a SPAN so all child generations (embedding / VLM /
	// chat / rerank / ASR) nest correctly in the Langfuse UI.
	mux.Use(langfuse.AsynqMiddleware())

	// Register extract handlers - router will dispatch to appropriate handler
	mux.HandleFunc(types.TypeChunkExtract, params.ChunkExtractor.Handle)
	mux.HandleFunc(types.TypeDataTableSummary, params.DataTableSummary.Handle)

	// Register document processing handler
	mux.HandleFunc(types.TypeDocumentProcess, params.KnowledgeService.ProcessDocument)

	// Register manual knowledge processing handler (cleanup + re-indexing)
	mux.HandleFunc(types.TypeManualProcess, params.KnowledgeService.ProcessManualUpdate)

	// Register FAQ import handler (includes dry run mode)
	mux.HandleFunc(types.TypeFAQImport, params.KnowledgeService.ProcessFAQImport)

	// Register question generation handler
	mux.HandleFunc(types.TypeQuestionGeneration, params.KnowledgeService.ProcessQuestionGeneration)

	// Register summary generation handler
	mux.HandleFunc(types.TypeSummaryGeneration, params.KnowledgeService.ProcessSummaryGeneration)

	// Register KB clone handler
	mux.HandleFunc(types.TypeKBClone, params.KnowledgeService.ProcessKBClone)

	// Register knowledge move handler
	mux.HandleFunc(types.TypeKnowledgeMove, params.KnowledgeService.ProcessKnowledgeMove)

	// Register knowledge list delete handler
	mux.HandleFunc(types.TypeKnowledgeListDelete, params.KnowledgeService.ProcessKnowledgeListDelete)

	// Register index delete handler
	mux.HandleFunc(types.TypeIndexDelete, params.TagService.ProcessIndexDelete)

	// Register KB delete handler
	mux.HandleFunc(types.TypeKBDelete, params.KnowledgeBaseService.ProcessKBDelete)

	// Register image multimodal handler
	mux.HandleFunc(types.TypeImageMultimodal, params.ImageMultimodal.Handle)

	// Register knowledge post process handler
	mux.HandleFunc(types.TypeKnowledgePostProcess, params.KnowledgePostProcess.Handle)

	// Register data source sync handler
	mux.HandleFunc(types.TypeDataSourceSync, params.DataSourceService.ProcessSync)

	// Register wiki ingest handler
	mux.HandleFunc(types.TypeWikiIngest, params.WikiIngest.Handle)

	go func() {
		// Start the server
		if err := params.Server.Run(mux); err != nil {
			log.Fatalf("could not run server: %v", err)
		}
	}()

	// Start the KnowledgeReconciler in the background. It detects rows
	// stuck in parse_status='processing' that no longer have a live
	// asynq task — e.g. because asynq exhausted retries (archived) or
	// the worker process crashed mid-execution before lease renewal.
	// Without this, such rows stay "processing" forever in Redis mode.
	if params.Reconciler != nil {
		go params.Reconciler.Run(context.Background())
	}

	return mux
}
