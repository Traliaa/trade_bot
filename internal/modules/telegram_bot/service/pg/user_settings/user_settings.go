package user_settings

import (
	"context"
	"fmt"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/telegram_bot/service/pg/user_settings/sql"

	"github.com/bytedance/sonic"
	"github.com/jackc/pgx/v5"
)

// UserSettings implement db store
type UserSettings struct {
	sql *sql.Queries
}

// New instance
func New() *UserSettings {
	return &UserSettings{
		sql: sql.New(),
	}
}

func (u *UserSettings) Insert(ctx context.Context, tx pgx.Tx, user *models.UserSettings) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("UserSettings.Insert: %w", err)
		}
	}()

	var data []byte
	data, err = sonic.Marshal(user.Settings)
	if err != nil {
		return err
	}
	_, err = u.sql.Insert(ctx, tx, &sql.InsertParams{
		Chatid:   user.UserID,
		Name:     user.Name,
		Settings: data,
		Step:     user.Step,
	})
	if err != nil {
		return err
	}
	return
}

func (u *UserSettings) Update(ctx context.Context, tx pgx.Tx, user *models.UserSettings) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("UserSettings.Update: %w", err)
		}
	}()
	var data []byte
	data, err = sonic.Marshal(user.Settings)
	if err != nil {
		return err
	}
	return u.sql.Update(ctx, tx, &sql.UpdateParams{
		Chatid:   user.UserID,
		Name:     user.Name,
		Settings: data,
		Step:     user.Step,
	})
}

func (u *UserSettings) Delete(ctx context.Context, tx pgx.Tx, user *models.UserSettings) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("UserSettings.Delete: %w", err)
		}
	}()
	return u.sql.Delete(ctx, tx, &sql.DeleteParams{
		Chatid: user.UserID,
		ID:     user.ID,
	})
}

func (u *UserSettings) GetById(ctx context.Context, tx pgx.Tx, chatID int64) (user *models.UserSettings, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("UserSettings.GetById: %w", err)
		}
	}()
	resp, err := u.sql.GetById(ctx, tx, chatID)
	if err != nil {
		return nil, err
	}

	var t models.Settings
	err = sonic.Unmarshal(resp.Settings, &t)
	if err != nil {
		return nil, err
	}
	return &models.UserSettings{
		ID:       resp.ID,
		UserID:   chatID,
		Name:     resp.Name,
		Settings: t,
		Step:     resp.Step,
	}, nil
}

//func (u *UserSettings) GetAll(ctx context.Context, tx pgx.Tx) (users []*models.UserSettings, err error) {
//	defer func() {
//		if err != nil {
//			err = fmt.Errorf("UserSettings.GetAll: %w", err)
//		}
//	}()
//	resp, err := u.sql.GetAll(ctx, tx)
//	if err != nil {
//		return nil, err
//	}
//	users = make([]*dto.UserSettings, len(resp))
//
//	for i := range resp {
//		users = append(users, &dto.UserSettings{
//			ID:       resp[i].ID,
//			ChatID:   resp[i].Chatid,
//			Name:     resp[i].Name,
//			AuthCode: resp[i].AuthCode,
//			Step:     resp[i].Step,
//		})
//	}
//	return users, nil
//}
