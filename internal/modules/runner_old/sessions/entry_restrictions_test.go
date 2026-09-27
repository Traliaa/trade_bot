package sessions

import (
	"context"
	"errors"
	"fmt"
	"testing"
	okx "trade_bot/internal/modules/okx_client/service"
)

type restrictionStoreFixture struct {
	rows        map[string]bool
	unavailable bool
}

func (s *restrictionStoreFixture) EntryBlocked(_ context.Context, u int64, i string) (bool, error) {
	if s.unavailable {
		return false, errors.New("storage offline")
	}
	return s.rows[fmt.Sprint(u)+":"+i], nil
}
func (s *restrictionStoreFixture) BlockEntry(_ context.Context, u int64, i, code string) (bool, error) {
	if s.unavailable {
		return false, errors.New("storage offline")
	}
	k := fmt.Sprint(u) + ":" + i
	first := !s.rows[k]
	s.rows[k] = true
	return first, nil
}

func TestEntryRestrictionsPersistAndAreAccountScoped(t *testing.T) {
	ctx := context.Background()
	store := &restrictionStoreFixture{rows: map[string]bool{}}
	var guard entryRestrictionGuard
	denied := fmt.Errorf("PlaceMarket: %w", &okx.TradeError{Code: "1", SCode: "51155"})
	err := guard.record(ctx, store, 7, "CL-USDT-SWAP", denied)
	var blocked *EntryBlockedError
	if !errors.As(err, &blocked) || !blocked.First {
		t.Fatalf("first refusal must notify: %v", err)
	}
	var restarted entryRestrictionGuard
	if !errors.Is(restarted.check(ctx, store, 7, "CL-USDT-SWAP"), ErrEntryBlocked) {
		t.Fatal("restriction lost after restart")
	}
	if err := restarted.check(ctx, store, 8, "CL-USDT-SWAP"); err != nil {
		t.Fatalf("other account blocked: %v", err)
	}
	if err := restarted.check(ctx, store, 7, "BTC-USDT-SWAP"); err != nil {
		t.Fatalf("other instrument blocked: %v", err)
	}
	err = restarted.record(ctx, store, 7, "CL-USDT-SWAP", denied)
	if !errors.As(err, &blocked) || blocked.First {
		t.Fatalf("repeated refusal must not notify: %v", err)
	}
}

func TestEntryRestrictionsDoNotBlacklistTransientOrSizingErrors(t *testing.T) {
	ctx := context.Background()
	store := &restrictionStoreFixture{rows: map[string]bool{}}
	var guard entryRestrictionGuard
	for _, err := range []error{context.DeadlineExceeded, errors.New("text mentioning 51155"), &okx.TradeError{Code: "1", SCode: "51121"}, &okx.TradeError{Code: "1", SCode: "51186"}, &okx.TradeError{Code: "1", SCode: "51008"}} {
		if got := guard.record(ctx, store, 7, "CL-USDT-SWAP", err); got != err {
			t.Fatalf("unrelated error changed: %v", got)
		}
		if got := guard.check(ctx, store, 7, "CL-USDT-SWAP"); got != nil {
			t.Fatalf("false restriction: %v", got)
		}
	}
}

func TestEntryRestrictionsFailClosedAndRetryPersistence(t *testing.T) {
	ctx := context.Background()
	store := &restrictionStoreFixture{rows: map[string]bool{}, unavailable: true}
	var guard entryRestrictionGuard
	if guard.check(ctx, store, 7, "CL-USDT-SWAP") == nil {
		t.Fatal("entry allowed without reading restriction state")
	}
	err := guard.record(ctx, store, 7, "CL-USDT-SWAP", &okx.TradeError{Code: "1", SCode: "51155"})
	if !errors.Is(err, ErrEntryBlocked) {
		t.Fatalf("lost rejection on write failure: %v", err)
	}
	store.unavailable = false
	if !errors.Is(guard.check(ctx, store, 7, "CL-USDT-SWAP"), ErrEntryBlocked) {
		t.Fatal("unpersisted rejection forgotten")
	}
	var restarted entryRestrictionGuard
	if !errors.Is(restarted.check(ctx, store, 7, "CL-USDT-SWAP"), ErrEntryBlocked) {
		t.Fatal("write was not retried after recovery")
	}
}
