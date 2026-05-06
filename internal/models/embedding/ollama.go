package embedding

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	ollamaapi "github.com/ollama/ollama/api"
)

// ollamaEmbedTimeout caps a single Embed RPC. A hung Ollama instance can
// leave a worker blocked indefinitely otherwise — at scale this exhausts
// the entire asynq pool. 60s is plenty for any reasonable batch size.
func ollamaEmbedTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("WEKNORA_EMBED_TIMEOUT_MS"))
	if v == "" {
		return 60 * time.Second
	}
	ms, err := strconv.Atoi(v)
	if err != nil || ms <= 0 {
		return 60 * time.Second
	}
	return time.Duration(ms) * time.Millisecond
}

// ollamaEmbedMaxAttempts is how many times to attempt a single Embed
// call (initial + retries). Default 2 (one retry only). Higher values
// magnify total wall time on flapping endpoints — combined with the
// 60s per-call timeout, attempts=2 gives at most ~120s + 1s backoff =
// 121s per batch, well within docProcess timeouts.
func ollamaEmbedMaxAttempts() int {
	v := strings.TrimSpace(os.Getenv("WEKNORA_EMBED_MAX_ATTEMPTS"))
	if v == "" {
		return 2
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 2
	}
	return n
}

// isTransientEmbedError returns true for errors that suggest retrying
// might help. context.Canceled propagates as-is (parent task cancelled,
// no point retrying). context.DeadlineExceeded IS transient: it means
// either our per-call timeout fired (retry handles this) or the parent
// task timeout fired (the next iteration's ctx.Err() check exits the
// loop). HTTP 5xx / 429 / connection-level errors also retry. We do NOT
// match generic substrings like "500" or "eof" — those collide with
// payload content.
func isTransientEmbedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	// Network-layer errors that warrant retry.
	netSignals := []string{
		"connection refused", "connection reset",
		"reset by peer", "broken pipe",
		"no route to host", "no such host",
		"i/o timeout", "tls handshake",
		"client.timeout",            // net/http Client.Timeout wording
		"context deadline exceeded", // wrapped ctx errors
	}
	for _, frag := range netSignals {
		if strings.Contains(msg, frag) {
			return true
		}
	}
	// HTTP server-side throttling / overload (matched as " <code>" or
	// "status <code>" to avoid colliding with chunk content).
	httpSignals := []string{
		"status 500", "status 502", "status 503", "status 504", "status 429",
		"http 500", "http 502", "http 503", "http 504", "http 429",
		"temporarily unavailable", "too many requests",
	}
	for _, frag := range httpSignals {
		if strings.Contains(msg, frag) {
			return true
		}
	}
	return false
}

// OllamaEmbedder implements text vectorization functionality using Ollama
type OllamaEmbedder struct {
	modelName            string
	truncatePromptTokens int
	ollamaService        *ollama.OllamaService
	dimensions           int
	modelID              string
	EmbedderPooler
}

// OllamaEmbedRequest represents an Ollama embedding request
type OllamaEmbedRequest struct {
	Model                string `json:"model"`
	Prompt               string `json:"prompt"`
	TruncatePromptTokens int    `json:"truncate_prompt_tokens"`
}

// OllamaEmbedResponse represents an Ollama embedding response
type OllamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// NewOllamaEmbedder creates a new Ollama embedder
func NewOllamaEmbedder(baseURL,
	modelName string,
	truncatePromptTokens int,
	dimensions int,
	modelID string,
	pooler EmbedderPooler,
	ollamaService *ollama.OllamaService,
) (*OllamaEmbedder, error) {
	if modelName == "" {
		modelName = "nomic-embed-text"
	}

	if truncatePromptTokens == 0 {
		truncatePromptTokens = 511
	}

	return &OllamaEmbedder{
		modelName:            modelName,
		truncatePromptTokens: truncatePromptTokens,
		ollamaService:        ollamaService,
		EmbedderPooler:       pooler,
		dimensions:           dimensions,
		modelID:              modelID,
	}, nil
}

// ensureModelAvailable ensures that the model is available
func (e *OllamaEmbedder) ensureModelAvailable(ctx context.Context) error {
	logger.GetLogger(ctx).Infof("Ensuring model %s is available", e.modelName)
	return e.ollamaService.EnsureModelAvailable(ctx, e.modelName)
}

// Embed converts text to vector
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	embedding, err := e.BatchEmbed(ctx, []string{text})
	if err != nil {
		return nil, fmt.Errorf("failed to embed text: %w", err)
	}

	if len(embedding) == 0 {
		return nil, fmt.Errorf("failed to embed text: %w", err)
	}

	return embedding[0], nil
}

// BatchEmbed converts multiple texts to vectors in batch
func (e *OllamaEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	// Ensure model is available
	if err := e.ensureModelAvailable(ctx); err != nil {
		return nil, err
	}

	// Create request
	req := &ollamaapi.EmbedRequest{
		Model:   e.modelName,
		Input:   texts,
		Options: make(map[string]interface{}),
	}

	// Set truncation parameters
	if e.truncatePromptTokens > 0 {
		req.Options["num_ctx"] = e.truncatePromptTokens
		truncate := true
		req.Truncate = &truncate
	}

	// Send request — bounded per-call timeout, with retry on transient
	// errors (network blips, Ollama overload, 5xx, 429). The combined
	// retry budget is bounded by the parent task timeout in any case.
	startTime := time.Now()
	maxAttempts := ollamaEmbedMaxAttempts()
	timeout := ollamaEmbedTimeout()
	var resp *ollamaapi.EmbedResponse
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		var attemptErr error
		resp, attemptErr = e.ollamaService.Embeddings(callCtx, req)
		cancel()
		if attemptErr == nil {
			break
		}
		lastErr = attemptErr
		if !isTransientEmbedError(attemptErr) || attempt == maxAttempts-1 {
			return nil, fmt.Errorf("failed to get embedding vectors: %w", attemptErr)
		}
		// Linear backoff with jitter: 1s, 2s, 3s, ... — keeps total
		// retry budget bounded by maxAttempts so a flapping endpoint
		// can't burn the parent task's timeout on a single batch.
		backoff := time.Duration(attempt+1) * time.Second
		jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
		logger.GetLogger(ctx).Warnf("ollama embed attempt %d/%d failed: %v, retrying in %v",
			attempt+1, maxAttempts, attemptErr, backoff+jitter)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff + jitter):
		}
	}
	if resp == nil {
		return nil, fmt.Errorf("failed to get embedding vectors after %d attempts: %w", maxAttempts, lastErr)
	}

	logger.GetLogger(ctx).Debugf("Embedding vector retrieval took: %v", time.Since(startTime))
	return resp.Embeddings, nil
}

// GetModelName returns the model name
func (e *OllamaEmbedder) GetModelName() string {
	return e.modelName
}

// GetDimensions returns the vector dimensions
func (e *OllamaEmbedder) GetDimensions() int {
	return e.dimensions
}

// GetModelID returns the model ID
func (e *OllamaEmbedder) GetModelID() string {
	return e.modelID
}
