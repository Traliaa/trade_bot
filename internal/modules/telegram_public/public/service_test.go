package public

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type notifierStub struct {
	calls int
	err   error
}

func (n *notifierStub) SendServiceText(context.Context, string) (int, error) {
	n.calls++
	return n.calls, n.err
}
func (n *notifierStub) EditServiceText(context.Context, int, string) error {
	panic("routine status must not edit Telegram messages")
}

func TestStartupIsSilent(t *testing.T) {
	n := &notifierStub{}
	s := NewService(n, nil)
	ctx := context.Background()
	s.Start(ctx)
	defer s.Stop()
	for _, state := range []State{StateRestarting, StateConnecting, StatePreparing, StatePreparing, StateReady} {
		if err := s.SendOrEdit(ctx, Status{State: state}); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := s.Snapshot()
	s.Heartbeat(ctx)
	after, events := s.Snapshot()
	if n.calls != 0 || after.State != StateReady || len(events) != 3 || before.UpdatedAt != after.UpdatedAt {
		t.Fatalf("calls=%d status=%+v events=%d", n.calls, after, len(events))
	}
}

func TestIncidentAndRecoveryAreDeduplicatedEvenThroughPreparation(t *testing.T) {
	n := &notifierStub{err: errors.New("Telegram unavailable")}
	s := NewService(n, nil)
	for _, state := range []State{StateError, StateError, StatePreparing, StateError, StatePreparing, StateReady, StateReady} {
		if err := s.SendOrEdit(context.Background(), Status{State: state}); err != nil {
			t.Fatalf("Telegram blocked readiness: %v", err)
		}
	}
	last, _ := s.Snapshot()
	if n.calls != 2 || last.State != StateReady {
		t.Fatalf("calls=%d state=%s", n.calls, last.State)
	}
}

func TestSnapshotIsBoundedAndIndependent(t *testing.T) {
	s := NewService(nil, nil)
	for i := 0; i < 100; i++ {
		state := StatePreparing
		if i%2 == 0 {
			state = StateReady
		}
		_ = s.SendOrEdit(context.Background(), Status{State: state})
	}
	_, events := s.Snapshot()
	if len(events) != 50 {
		t.Fatalf("events=%d", len(events))
	}
	events[0].State = StateError
	_, fresh := s.Snapshot()
	if fresh[0].State == StateError {
		t.Fatal("snapshot exposed mutable history")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.SendOrEdit(context.Background(), Status{State: StatePreparing})
			s.Snapshot()
		}()
	}
	wg.Wait()
}
