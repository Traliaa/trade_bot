package user_settings

import (
	"context"
	"fmt"
	"trade_bot/internal/models"
	sql2 "trade_bot/internal/modules/repository/pg/user_settings/sql"

	"github.com/bytedance/sonic"
	"github.com/jackc/pgx/v5"
	"github.com/samber/lo"
)

// UserSettings implement db store
type UserSettings struct {
	sql *sql2.Queries
}

// New instance
func New() *UserSettings {
	return &UserSettings{
		sql: sql2.New(),
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
	_, err = u.sql.Insert(ctx, tx, &sql2.InsertParams{
		Chatid:   user.UserID,
		Name:     user.Name,
		Settings: data,
		Step:     user.Step,
		Status:   user.Status,
		Premium:  user.Premium,
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
	return u.sql.Update(ctx, tx, &sql2.UpdateParams{
		Chatid:   user.UserID,
		Name:     user.Name,
		Settings: data,
		Step:     user.Step,
		Status:   user.Status,
		Premium:  user.Premium,
	})
}

func (u *UserSettings) Delete(ctx context.Context, tx pgx.Tx, user *models.UserSettings) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("UserSettings.Delete: %w", err)
		}
	}()
	return u.sql.Delete(ctx, tx, &sql2.DeleteParams{
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
		Status:   resp.Status,
		Premium:  resp.Premium,
	}, nil
}

//	func (u *UserSettings) GetAll(ctx context.Context, tx pgx.Tx) (users []*models.UserSettings, err error) {
//		defer func() {
//			if err != nil {
//				err = fmt.Errorf("UserSettings.GetAll: %w", err)
//			}
//		}()
//		resp, err := u.sql.GetAll(ctx, tx)
//		if err != nil {
//			return nil, err
//		}
//		users = make([]*dto.UserSettings, len(resp))
//
//		for i := range resp {
//			users = append(users, &dto.UserSettings{
//				ID:       resp[i].ID,
//				ChatID:   resp[i].Chatid,
//				Name:     resp[i].Name,
//				AuthCode: resp[i].AuthCode,
//				Step:     resp[i].Step,
//			})
//		}
//		return users, nil
//	}
//
// ListEnabled возвращает пользователей, у которых Enabled=true
func (u *UserSettings) ListEnabled(ctx context.Context, tx pgx.Tx) (users []*models.UserSettings, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("pg.User.ListEnabled: %w", err)
		}
	}()

	resp, err := u.sql.ListEnabled(ctx, tx)
	if err != nil {
		return nil, err
	}
	for i := range resp {
		var t models.Settings
		err = sonic.Unmarshal(resp[i].Settings, &t)
		if err != nil {
			return nil, err
		}
		users = append(users, &models.UserSettings{
			ID:       resp[i].ID,
			UserID:   resp[i].Chatid,
			Name:     resp[i].Name,
			Settings: t,
			Step:     lo.FromPtr(resp[i].Step),
			Status:   resp[i].Status,
			Premium:  resp[i].Premium,
		})
	}
	return users, err
}
