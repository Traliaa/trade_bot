package public

import (
	"context"
	"errors"
	"testing"
)

type historyStub struct {
	events []Status
	fail   bool
}

func (s *historyStub) Append(_ context.Context, st Status) error {
	if s.fail {
		return errors.New("database unavailable")
	}
	s.events = append(s.events, st)
	return nil
}
func (s *historyStub) Recent(context.Context) ([]Status, error) {
	if s.fail {
		return nil, errors.New("database unavailable")
	}
	return s.events, nil
}
func TestHistorySurvivesServiceRecreationAndFallsBackOnFailure(t *testing.T) {
	store := &historyStub{}
	s := NewService(nil, nil)
	s.SetHistoryStore(store)
	_ = s.SendOrEdit(context.Background(), Status{State: StatePreparing})
	_ = s.SendOrEdit(context.Background(), Status{State: StatePreparing, Progress: 25})
	_ = s.SendOrEdit(context.Background(), Status{State: StateReady})
	if len(store.events) != 2 {
		t.Fatal("progress updates polluted persistent history")
	}
	next := NewService(nil, nil)
	next.SetHistoryStore(store)
	events, persistent := next.History(context.Background())
	if !persistent || len(events) != 2 {
		t.Fatal("history lost on restart")
	}
	store.fail = true
	if err := next.SendOrEdit(context.Background(), Status{State: StatePreparing}); err != nil {
		t.Fatal("history failure blocked startup")
	}
	events, persistent = next.History(context.Background())
	if persistent || len(events) != 1 || events[0].State != StatePreparing {
		t.Fatal("no honest in-memory fallback")
	}
}
