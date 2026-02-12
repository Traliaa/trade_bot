package public

import (
	"context"
	"sync"
)

type Meta struct {
	MessageID int
}

type Repo interface {
	Get(ctx context.Context) (Meta, bool, error)
	Upsert(ctx context.Context, meta Meta) error
}

type InMemoryRepo struct {
	mu   sync.Mutex
	meta Meta
	ok   bool
}

func NewInMemoryRepo() *InMemoryRepo { return &InMemoryRepo{} }

func (r *InMemoryRepo) Get(ctx context.Context) (Meta, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.meta, r.ok, nil
}

func (r *InMemoryRepo) Upsert(ctx context.Context, meta Meta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta = meta
	r.ok = true
	return nil
}
