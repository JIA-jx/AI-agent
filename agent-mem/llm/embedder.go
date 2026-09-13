package llm

import "context"

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

type embedderAdapter struct {
	c Client
}

type completerAdapter struct {
	c Client
}

func NewEmbedder(c Client) Embedder {
	return embedderAdapter{c: c}
}

func NewCompleter(c Client) Completer {
	return completerAdapter{c: c}
}

func (a embedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return a.c.Embed(ctx, text)
}

func (a completerAdapter) Complete(ctx context.Context, system, user string) (string, error) {
	return a.c.Complete(ctx, system, user)
}
