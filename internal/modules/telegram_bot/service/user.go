package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"trade_bot/internal/models"
)

func (t *Telegram) getUser(ctx context.Context, chatID int64) (*models.UserSettings, error) {
	user, err := t.repo.Get(ctx, chatID)
	if err != nil {
		// not found в PG
		if errors.Is(err, sql.ErrNoRows) {
			user = models.NewTradingSettingsFromDefaults(chatID, t.cfg)
			if err := t.repo.Create(ctx, user); err != nil {
				return nil, fmt.Errorf("create user settings: %w", err)
			}
			return user, nil
		}

		// любая другая ошибка — пробрасываем
		return nil, fmt.Errorf("get user settings: %w", err)
	}

	// ✅ not found в file-repo (обычно: user=nil, err=nil)
	if user == nil {
		user = models.NewTradingSettingsFromDefaults(chatID, t.cfg)
		if err := t.repo.Create(ctx, user); err != nil {
			return nil, fmt.Errorf("create user settings: %w", err)
		}
		return user, nil
	}

	return user, nil
}
