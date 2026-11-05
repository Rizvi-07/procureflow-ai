package assistant

import (
	"context"
	"fmt"
)

type IndexWriter interface {
	Existing(context.Context) (map[string]Chunk, error)
	Replace(context.Context, []Chunk) error
}

func Import(ctx context.Context, store IndexWriter, provider Provider, chunks []Chunk, dimensions int) (int, error) {
	if len(chunks) < 1 || len(chunks) > MaxChunks {
		return 0, fmt.Errorf("invalid policy corpus size")
	}
	existing, err := store.Existing(ctx)
	if err != nil {
		return 0, err
	}
	staged := make([]Chunk, len(chunks))
	copy(staged, chunks)
	embedded := 0
	for i, c := range staged {
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		old, found := existing[chunkKey(c)]
		if found && old.Hash == c.Hash {
			staged[i].Embedding = old.Embedding
		} else {
			staged[i].Embedding, err = provider.Embed(ctx, EmbeddingText(c))
			if err != nil {
				return 0, err
			}
			embedded++
		}
		if err = ValidateVector(staged[i].Embedding, dimensions); err != nil {
			return 0, err
		}
	}
	if err = store.Replace(ctx, staged); err != nil {
		return 0, err
	}
	return embedded, nil
}
