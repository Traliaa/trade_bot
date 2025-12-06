package pg

import (
	"context"
	"fmt"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/telegram_bot/service/pg/user_settings"
	"trade_bot/pkg/db"

	"github.com/jackc/pgx/v5"
)

type User struct {
	db   *db.PgTxManager
	user *user_settings.UserSettings
}

// NewUser instance
func NewUser(db *db.PgTxManager) *User {
	return &User{
		db:   db,
		user: user_settings.New(),
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
	err = u.db.RunMaster(ctx,
		func(ctxTx context.Context, tx pgx.Tx) error {
			err = u.user.Insert(ctx, tx, user)
			if err != nil {
				return err
			}
			return nil
		})
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
	err = u.db.RunMaster(ctx,
		func(ctxTx context.Context, tx pgx.Tx) error {
			return u.user.Update(ctx, tx, user)
		})
	return err
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

	err = u.db.RunMaster(ctx,
		func(ctxTx context.Context, tx pgx.Tx) error {
			user, err = u.user.GetById(ctx, tx, userID)
			if err != nil {
				return err
			}
			return nil
		})

	return user, err
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
	err = u.db.RunMaster(ctx,
		func(ctxTx context.Context, tx pgx.Tx) error {
			return u.user.Delete(ctx, tx, user)
		})
	return err
}
