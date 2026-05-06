package service

import (
	"errors"
	"os"
	"strconv"
	"strings"
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

// Per-task timeouts.
//
// Without these, asynq's default 30-minute task timeout applies to every
// handler. For a Knowledge with thousands of chunks, sequential AIGS or
// Wiki ingest can run longer; conversely, ProcessDocument should usually
// finish within minutes and a 30-minute hang likely means a stuck Ollama
// or DocReader call. Per-task budgets bound worker hold time.
//
// All defaults are overridable. They preserve existing behaviour for small
// setups and only matter at scale.

func docProcessTimeout() time.Duration {
	return envDurationDefault("WEKNORA_DOC_PROCESS_TIMEOUT", 30*time.Minute)
}

func qgTimeout() time.Duration {
	return envDurationDefault("WEKNORA_QG_TIMEOUT", 60*time.Minute)
}

func summaryTimeout() time.Duration {
	return envDurationDefault("WEKNORA_SUMMARY_TIMEOUT", 30*time.Minute)
}

func postProcessTimeout() time.Duration {
	return envDurationDefault("WEKNORA_POSTPROCESS_TIMEOUT", 10*time.Minute)
}

func imageMultimodalTimeout() time.Duration {
	return envDurationDefault("WEKNORA_IMAGE_TIMEOUT", 20*time.Minute)
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
