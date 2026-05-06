package service

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hibiken/asynq"
)

// Deterministic task IDs let the reconciler distinguish "live task in
// asynq" from "orphan in DB". They also collapse duplicate enqueues (the
// same Knowledge being uploaded twice in quick succession or re-enqueued
// by the reconciler while a previous attempt is still pending) into a
// single task — see EnqueueIfNotExists.
//
// Each helper namespaces by operation so that, e.g., a document-process
// task and a question-generation task for the same Knowledge can co-exist
// without colliding.

// DocProcessTaskID returns the deterministic asynq task ID for a
// TypeDocumentProcess (or TypeManualProcess) task. Used by the reconciler
// to look up live tasks for a given knowledge.
func DocProcessTaskID(knowledgeID string) string  { return "doc-process-" + knowledgeID }
func ManualProcessTaskID(knowledgeID string) string { return "manual-process-" + knowledgeID }
func PostProcessTaskID(knowledgeID string) string  { return "post-process-" + knowledgeID }
func QGTaskID(knowledgeID string) string           { return "qg-" + knowledgeID }
func SummaryTaskID(knowledgeID string) string      { return "summary-" + knowledgeID }

// IsTaskIDConflict reports whether err signals that the task ID is
// already in use — semantically: "another enqueue already covered this
// work, so we can treat it as success."
func IsTaskIDConflict(err error) bool {
	return errors.Is(err, asynq.ErrTaskIDConflict)
}

// ErrTaskBusy signals that a deterministic-ID task is currently running
// (asynq's DeleteTask refuses to delete active tasks). Callers must
// abort destructive ops like cleanupKnowledgeResources when this fires
// — letting the running task finish first is safer than racing it.
var ErrTaskBusy = errors.New("task is currently executing")

// inspectorOnce + inspectorVal: process-wide singleton. asynq.Inspector
// holds a Redis client; reusing one connection is far cheaper than
// creating one per ReparseKnowledge call (which the recover endpoint
// can fan out to 100+ in a single request).
var (
	inspectorOnce sync.Once
	inspectorVal  *asynq.Inspector
)

// sharedInspector returns the process-wide inspector. nil in Lite mode.
func sharedInspector() *asynq.Inspector {
	inspectorOnce.Do(func() {
		inspectorVal = buildInspector()
	})
	return inspectorVal
}

// buildInspector constructs an *asynq.Inspector against the configured
// Redis broker. Returns nil when REDIS_ADDR is not set (Lite mode),
// signalling the caller to skip Inspector-based logic.
func buildInspector() *asynq.Inspector {
	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		return nil
	}
	db := 0
	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		if parsed, err := strconv.Atoi(dbStr); err == nil {
			db = parsed
		}
	}
	readMs := 3000
	writeMs := 3000
	if v := strings.TrimSpace(os.Getenv("WEKNORA_REDIS_READ_TIMEOUT_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			readMs = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("WEKNORA_REDIS_WRITE_TIMEOUT_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			writeMs = n
		}
	}
	return asynq.NewInspector(&asynq.RedisClientOpt{
		Addr:         addr,
		Username:     os.Getenv("REDIS_USERNAME"),
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           db,
		ReadTimeout:  time.Duration(readMs) * time.Millisecond,
		WriteTimeout: time.Duration(writeMs) * time.Millisecond,
	})
}

// killTaskByDeterministicID deletes a task with the given deterministic
// ID across all queues. Returns nil when the task was deleted or never
// existed; returns ErrTaskBusy when at least one queue reported the
// task was active (asynq's DeleteTask returns a non-typed error in that
// case — anything that's not ErrTaskNotFound / ErrQueueNotFound is
// treated as "active or broken broker"). The caller MUST abort
// destructive operations (cleanup, status reset, fresh enqueue) on a
// non-nil return so a still-running worker can finish without racing
// against state we just wiped. No-op (returns nil) in Lite mode.
func killTaskByDeterministicID(taskID string) error {
	insp := sharedInspector()
	if insp == nil {
		return nil
	}
	for _, q := range []string{"critical", "default", "low"} {
		err := insp.DeleteTask(q, taskID)
		if err == nil {
			continue
		}
		if errors.Is(err, asynq.ErrTaskNotFound) || errors.Is(err, asynq.ErrQueueNotFound) {
			continue
		}
		return fmt.Errorf("delete %s in queue %s: %w", taskID, q, ErrTaskBusy)
	}
	return nil
}

// envDurationDefault parses a duration env var like "30m" or "1800s".
// Falls back to def when unset, empty, or unparseable.
func envDurationDefault(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	parsed, err := time.ParseDuration(v)
	if err != nil || parsed <= 0 {
		return def
	}
	return parsed
}

// envIntDefault returns the integer env var or def when unset / invalid /
// negative. Used for tunables like WEKNORA_QG_CONCURRENCY.
func envIntDefault(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// Per-task timeouts.
//
// asynq's library default (when no Timeout option is passed) is 30 minutes.
// We set explicit values so we can a) raise the cap for long-running QG
// over thousands of chunks and b) keep the orchestration tasks at the
// existing 30-minute floor without surprising drops below it. Defaults
// must NEVER be lower than asynq's 30-minute default — that would be a
// silent regression for workloads that worked before.

func docProcessTimeout() time.Duration {
	return envDurationDefault("WEKNORA_DOC_PROCESS_TIMEOUT", 30*time.Minute)
}

func qgTimeout() time.Duration {
	return envDurationDefault("WEKNORA_QG_TIMEOUT", 60*time.Minute)
}

func summaryTimeout() time.Duration {
	return envDurationDefault("WEKNORA_SUMMARY_TIMEOUT", 30*time.Minute)
}

// postProcessTimeout — orchestration only (DB reads + a handful of
// Enqueue calls). 30m is far more than needed but matches asynq default
// to avoid regressing any workload that previously got the implicit 30m.
func postProcessTimeout() time.Duration {
	return envDurationDefault("WEKNORA_POSTPROCESS_TIMEOUT", 30*time.Minute)
}

// imageMultimodalTimeout — VLM OCR + Caption per image. Some VLMs
// running locally on CPU can take many minutes per image. 30m default
// matches the asynq fallback; raise for slower hosts.
func imageMultimodalTimeout() time.Duration {
	return envDurationDefault("WEKNORA_IMAGE_TIMEOUT", 30*time.Minute)
}

// llmCallTimeout caps a single LLM Chat call (Question Generation,
// Summary). Independent of the asynq task budget: task budget = "all up
// time", per-call budget = "single round-trip". Without this, a hung
// model server pins the worker until the task timeout fires.
func llmCallTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("WEKNORA_LLM_TIMEOUT_MS"))
	if v == "" {
		return 3 * time.Minute
	}
	ms, err := strconv.Atoi(v)
	if err != nil || ms <= 0 {
		return 3 * time.Minute
	}
	return time.Duration(ms) * time.Millisecond
}

// docProcessOpts returns the standard asynq options for document processing
// tasks (TypeDocumentProcess, TypeManualProcess). Use when enqueueing a
// task to ensure the worker hold time is bounded.
func docProcessOpts(extra ...asynq.Option) []asynq.Option {
	return append([]asynq.Option{
		asynq.Queue("default"),
		asynq.MaxRetry(3),
		asynq.Timeout(docProcessTimeout()),
	}, extra...)
}

func qgOpts(extra ...asynq.Option) []asynq.Option {
	return append([]asynq.Option{
		asynq.Queue("low"),
		asynq.MaxRetry(3),
		asynq.Timeout(qgTimeout()),
	}, extra...)
}

func summaryOpts(extra ...asynq.Option) []asynq.Option {
	return append([]asynq.Option{
		asynq.Queue("low"),
		asynq.MaxRetry(3),
		asynq.Timeout(summaryTimeout()),
	}, extra...)
}

func postProcessOpts(extra ...asynq.Option) []asynq.Option {
	return append([]asynq.Option{
		asynq.Queue("default"),
		asynq.MaxRetry(3),
		asynq.Timeout(postProcessTimeout()),
	}, extra...)
}

func imageMultimodalOpts(extra ...asynq.Option) []asynq.Option {
	return append([]asynq.Option{
		asynq.Timeout(imageMultimodalTimeout()),
		asynq.MaxRetry(3),
	}, extra...)
}
