package embedding

import (
	"context"
	"os"
	"strconv"
	"sync"

	"github.com/Tencent/WeKnora/internal/models/utils"
	"github.com/panjf2000/ants/v2"
)

type batchEmbedder struct {
	pool *ants.Pool
}

func NewBatchEmbedder(pool *ants.Pool) EmbedderPooler {
	return &batchEmbedder{pool: pool}
}

type textEmbedding struct {
	text    string
	results []float32
}

func (e *batchEmbedder) BatchEmbedWithPool(ctx context.Context, model Embedder, texts []string) ([][]float32, error) {
	// Create goroutine pool for concurrent processing of document chunks
	var wg sync.WaitGroup
	var mu sync.Mutex  // For synchronizing access to error
	var firstErr error // Record the first error that occurs
	// Default 16 is a reasonable middle ground: small enough to not blow
	// up on tiny embed models, large enough to keep modern batch-friendly
	// embedders (Nemotron, BGE-M3, OpenAI) busy. Recommended override for
	// Nemotron-class models: 32-64. Set BATCH_EMBED_SIZE to tune.
	batchSize := 16
	if v := os.Getenv("BATCH_EMBED_SIZE"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			batchSize = parsed
		}
	}
	textEmbeddings := utils.MapSlice(texts, func(text string) *textEmbedding {
		return &textEmbedding{text: text}
	})

	// Function to process each document chunk
	processChunk := func(texts []*textEmbedding) func() {
		return func() {
			defer wg.Done()
			// If the parent ctx is already done, skip the embedding call
			// outright so a cancelled task doesn't keep firing requests.
			if ctxErr := ctx.Err(); ctxErr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = ctxErr
				}
				mu.Unlock()
				return
			}
			// If an error has already occurred, don't continue processing
			mu.Lock()
			alreadyFailed := firstErr != nil
			mu.Unlock()
			if alreadyFailed {
				return
			}
			// Embed text
			embedding, err := model.BatchEmbed(ctx, utils.MapSlice(texts, func(text *textEmbedding) string {
				return text.text
			}))
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			for i, text := range texts {
				if text == nil {
					continue
				}
				text.results = embedding[i]
			}
			mu.Unlock()
		}
	}

	// Submit all tasks to the goroutine pool
	for _, texts := range utils.ChunkSlice(textEmbeddings, batchSize) {
		wg.Add(1)
		err := e.pool.Submit(processChunk(texts))
		if err != nil {
			return nil, err
		}
	}

	// Wait for all tasks to complete
	wg.Wait()

	// Check if any errors occurred
	if firstErr != nil {
		return nil, firstErr
	}

	results := utils.MapSlice(textEmbeddings, func(text *textEmbedding) []float32 {
		return text.results
	})
	return results, nil
}
