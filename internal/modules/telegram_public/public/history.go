package public

import (
	"context"
	"trade_bot/pkg/db"
)

// History deliberately excludes error text, credentials and account data.
type HistoryStore interface {
	Append(context.Context, Status) error
	Recent(context.Context) ([]Status, error)
}
type PostgresHistoryStore struct{ manager *db.PgTxManager }

func NewPostgresHistoryStore(manager *db.PgTxManager) *PostgresHistoryStore {
	return &PostgresHistoryStore{manager: manager}
}
func (s *PostgresHistoryStore) Append(ctx context.Context, st Status) error {
	_, err := s.manager.Conn().Exec(ctx, `INSERT INTO public.service_status_events (state,occurred_at,instruments,progress) VALUES ($1,$2,$3,$4)`, st.State, st.UpdatedAt, st.Instruments, st.Progress)
	return err
}
func (s *PostgresHistoryStore) Recent(ctx context.Context) ([]Status, error) {
	rows, err := s.manager.Conn().Query(ctx, `SELECT state,occurred_at,instruments,progress FROM (SELECT id,state,occurred_at,instruments,progress FROM public.service_status_events ORDER BY occurred_at DESC,id DESC LIMIT 50) recent ORDER BY occurred_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Status{}
	for rows.Next() {
		var st Status
		if err := rows.Scan(&st.State, &st.UpdatedAt, &st.Instruments, &st.Progress); err != nil {
			return nil, err
		}
		result = append(result, st)
	}
	return result, rows.Err()
}

func (s *Service) SetHistoryStore(store HistoryStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = store
}

func (s *Service) History(ctx context.Context) ([]Status, bool) {
	s.mu.Lock()
	store := s.history
	s.mu.Unlock()
	if store != nil {
		if events, err := store.Recent(ctx); err == nil {
			return events, true
		}
	}
	_, events := s.Snapshot()
	return events, false
}
