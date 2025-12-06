package pg

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"trade_bot/internal/models"
	"trade_bot/pkg/db"
)

type User struct {
	db *db.PgTxManager

	mu   sync.RWMutex
	data map[int64]*models.UserSettings
}

// NewUser instance
func NewUser() *User {
	return &User{
		//db:   db,
		//user: user_settings.New(),
		data: make(map[int64]*models.UserSettings),
	}
}

// Create in db
func (u *User) Create(
	ctx context.Context,
	user *models.UserSettings,
) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.CreateEvent: %w", err)
		}
	}()
	//err = u.db.RunMaster(ctx,
	//	func(ctxTx context.Context, tx pgx.Tx) error {
	//		out, err = u.user.Insert(ctx, tx, user)
	//		if err != nil {
	//			return err
	//		}
	//		return nil
	//	})
	u.mu.Lock()
	defer u.mu.Unlock()
	u.data[user.UserID] = user
	return nil

}

// Update in db
func (u *User) Update(
	ctx context.Context,
	user *models.UserSettings,
) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.CreateEvent: %w", err)
		}
	}()
	//err = u.db.RunMaster(ctx,
	//	func(ctxTx context.Context, tx pgx.Tx) error {
	//		return u.user.Update(ctx, tx, user)
	//	})

	u.mu.Lock()
	defer u.mu.Unlock()
	u.data[user.UserID] = user
	return nil
}

// Get in db
func (u *User) Get(
	ctx context.Context,
	userID int64,
) (user *models.UserSettings, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.CreateEvent: %w", err)
		}
	}()

	u.mu.RLock()
	defer u.mu.RUnlock()
	user, ok := u.data[userID]
	if !ok {
		return nil, sql.ErrNoRows
	}

	//err = u.db.RunMaster(ctx,
	//	func(ctxTx context.Context, tx pgx.Tx) error {
	//		user, err = u.user.GetById(ctx, tx, ChatID)
	//		if err != nil {
	//			return err
	//		}
	//		return nil
	//	})

	return user, nil
}

// Delete in db
func (u *User) Delete(
	ctx context.Context,
	user *models.UserSettings,
) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.CreateEvent: %w", err)
		}
	}()
	//err = u.db.RunMaster(ctx,
	//	func(ctxTx context.Context, tx pgx.Tx) error {
	//		return u.user.Delete(ctx, tx, user)
	//	})

	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.data, user.UserID)

	return nil
}
