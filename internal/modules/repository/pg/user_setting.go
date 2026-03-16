package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/repository/pg/user_settings"
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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.Get: %w", err)
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

func (u *User) GetUser(ctx context.Context, chatID int64, cfg *config.Config) (*models.UserSettings, error) {

	user, err := u.Get(ctx, chatID)
	if err != nil {
		// not found в PG
		if errors.Is(err, sql.ErrNoRows) {
			user = models.NewTradingSettingsFromDefaults(chatID, cfg)
			if err := u.Create(ctx, user); err != nil {
				return nil, fmt.Errorf("create user settings: %w", err)
			}
			return user, nil
		}

		// любая другая ошибка — пробрасываем
		return nil, fmt.Errorf("get user settings: %w", err)
	}

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
	err = u.db.RunMaster(ctx,
		func(ctxTx context.Context, tx pgx.Tx) error {
			return u.user.Delete(ctx, tx, user)
		})
	return err
}

// ListEnabled возвращает пользователей, у которых Enabled=true
func (u *User) ListEnabled(ctx context.Context) (users []*models.UserSettings, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListEnabled: %w", err)
		}
	}()

	err = u.db.RunMaster(ctx, func(ctxTx context.Context, tx pgx.Tx) error {
		users, err = u.user.ListEnabled(ctxTx, tx)
		return err
	})
	return users, err
}
