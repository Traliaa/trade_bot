package sessions

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrEntryBlocked = errors.New("instrument excluded from new entries")

type EntryBlockedError struct {
	InstID         string
	First          bool // Only the first observed restriction warrants a Telegram notice.
	PersistenceErr error
}

func (e *EntryBlockedError) Error() string {
	if e.PersistenceErr != nil {
		return fmt.Sprintf("%s: %s (persistence failed: %v)", e.InstID, ErrEntryBlocked, e.PersistenceErr)
	}
	return fmt.Sprintf("%s: %s (OKX 51155)", e.InstID, ErrEntryBlocked)
}
func (e *EntryBlockedError) Unwrap() error { return ErrEntryBlocked }

type entryRestrictionStore interface {
	EntryBlocked(context.Context, int64, string) (bool, error)
	BlockEntry(context.Context, int64, string, string) (bool, error)
}
type entryRestrictionKey struct {
	userID int64
	instID string
}
type entryRestrictionGuard struct {
	mu sync.Mutex
	// Keep unsaved rejections in memory and retry persistence before any entry.
	pending map[entryRestrictionKey]string
}

func (g *entryRestrictionGuard) check(ctx context.Context, store entryRestrictionStore, userID int64, instID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	key := entryRestrictionKey{userID, instID}
	if code, ok := g.pending[key]; ok {
		_, err := store.BlockEntry(ctx, userID, instID, code)
		if err == nil {
			delete(g.pending, key)
		}
		return &EntryBlockedError{InstID: instID, PersistenceErr: err}
	}
	blocked, err := store.EntryBlocked(ctx, userID, instID)
	if err != nil {
		return fmt.Errorf("cannot verify entry restrictions: %w", err)
	}
	if blocked {
		return &EntryBlockedError{InstID: instID}
	}
	return nil
}

func (g *entryRestrictionGuard) record(ctx context.Context, store entryRestrictionStore, userID int64, instID string, orderErr error) error {
	var rejection interface {
		error
		EntryRestrictionCode() string
	}
	if !errors.As(orderErr, &rejection) || rejection.EntryRestrictionCode() == "" {
		return orderErr
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	key := entryRestrictionKey{userID, instID}
	_, pending := g.pending[key]
	code := rejection.EntryRestrictionCode()
	inserted, err := store.BlockEntry(ctx, userID, instID, code)
	if err != nil {
		if g.pending == nil {
			g.pending = make(map[entryRestrictionKey]string)
		}
		g.pending[key] = code
	} else {
		delete(g.pending, key)
	}
	return &EntryBlockedError{InstID: instID, First: !pending && (inserted || err != nil), PersistenceErr: err}
}

// CheckEntryAllowed is used only by entry paths. Exit/protection paths never
// consult this gate, even when the restriction store is unavailable.
func (s *UserSession) CheckEntryAllowed(ctx context.Context, instID string) error {
	if s.Repo == nil || s.User == nil {
		return errors.New("entry restriction storage unavailable")
	}
	return s.entryRestrictions.check(ctx, s.Repo, s.User.TelegramID, instID)
}
