package service

import (
	"sync/atomic"
	"time"
)

type State struct {
	ready     atomic.Bool
	startedAt time.Time

	wsConnected  atomic.Bool
	lastTickUnix atomic.Int64 // unix seconds
}

func NewState() *State {
	s := &State{startedAt: time.Now()}
	s.ready.Store(false)
	return s
}

func (s *State) SetReady(v bool) { s.ready.Store(v) }
func (s *State) Ready() bool     { return s.ready.Load() }

func (s *State) SetWSConnected(v bool) { s.wsConnected.Store(v) }
func (s *State) WSConnected() bool     { return s.wsConnected.Load() }

func (s *State) TouchTick(t time.Time) { s.lastTickUnix.Store(t.Unix()) }
func (s *State) LastTick() time.Time {
	u := s.lastTickUnix.Load()
	if u == 0 {
		return time.Time{}
	}
	return time.Unix(u, 0)
}

func (s *State) Uptime() time.Duration { return time.Since(s.startedAt) }
