package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"math"
	"strings"
	"time"
	"trade_bot/internal/models"
	okx "trade_bot/internal/modules/okx_client/service"
	"trade_bot/internal/modules/runner_old/sessions"
)

type closeRepository interface {
	GetByGUID(context.Context, uuid.UUID) (*models.TradeRecord, error)
	ClaimManualClose(context.Context, int64, models.ManualClose) (models.ManualClose, bool, error)
	GetManualClose(context.Context, int64, uuid.UUID, uuid.UUID) (models.ManualClose, error)
	SaveManualClose(context.Context, models.ManualClose) error
}
type closeExchange interface {
	ClosingPosition(context.Context, string, string) (float64, error)
	GetInstrumentMeta(context.Context, string) (models.Instrument, error)
	SubmitManualClose(context.Context, string, string, float64, string) (string, error)
	ManualCloseState(context.Context, string, string) (string, string, error)
}

func closeSize(position, fraction float64, meta models.Instrument) (float64, error) {
	for _, v := range []float64{position, fraction, meta.LotSz, meta.MinSz} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			return 0, fmt.Errorf("некорректный объём позиции")
		}
	}
	if fraction > 1 {
		return 0, fmt.Errorf("доля закрытия должна быть от 0 до 1")
	}
	size := math.Floor(position*fraction/meta.LotSz+1e-9) * meta.LotSz
	if size < meta.MinSz || size > position+meta.LotSz*1e-8 {
		return 0, fmt.Errorf("объём меньше минимального шага биржи")
	}
	if fraction < 1 && (position-size < meta.MinSz-1e-9 || size >= position) {
		return 0, fmt.Errorf("для частичного закрытия позиция слишком мала; выберите полное закрытие")
	}
	if meta.MaxMktSz > 0 && size > meta.MaxMktSz {
		return 0, fmt.Errorf("объём превышает лимит market-ордера; уменьшите долю")
	}
	return size, nil
}

func ownedTrade(ctx context.Context, repo closeRepository, userID int64, guid uuid.UUID) (*models.TradeRecord, error) {
	tr, err := repo.GetByGUID(ctx, guid)
	if err != nil || tr == nil || tr.UserID != userID {
		return nil, models.ErrCloseNotFound
	}
	return tr, nil
}

func executeManualClose(ctx context.Context, repo closeRepository, exchange closeExchange, userID int64, m models.ManualClose) (models.ManualClose, error) {
	if m.RequestID == uuid.Nil || m.Fraction <= 0 || m.Fraction > 1 || math.IsNaN(m.Fraction) || math.IsInf(m.Fraction, 0) {
		return m, fmt.Errorf("некорректный запрос закрытия")
	}
	tr, err := ownedTrade(ctx, repo, userID, m.TradeGUID)
	if err != nil {
		return m, err
	}
	if tr.Payload.PosSide != "long" && tr.Payload.PosSide != "short" {
		return m, models.ErrCloseConflict
	}
	m, claimed, err := repo.ClaimManualClose(ctx, userID, m)
	if err != nil || !claimed {
		return m, err
	}
	// Save final state independently of a disconnected HTTP caller.
	save := func() {
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if e := repo.SaveManualClose(saveCtx, m); e != nil {
			m.Status = "unknown"
			m.Message = "Не удалось записать результат. Проверьте состояние операции."
		}
	}
	position, err := exchange.ClosingPosition(ctx, tr.InstID, tr.Payload.PosSide)
	var meta models.Instrument
	if err == nil {
		meta, err = exchange.GetInstrumentMeta(ctx, tr.InstID)
	}
	if err == nil {
		m.Size, err = closeSize(position, m.Fraction, meta)
	}
	if err != nil {
		m.Status = "rejected"
		m.Message = err.Error()
		save()
		return m, nil
	}
	// One submission only. Even transport errors are ambiguous, so do not retry.
	m.OrderID, err = exchange.SubmitManualClose(ctx, tr.InstID, tr.Payload.PosSide, m.Size, strings.ReplaceAll(m.RequestID.String(), "-", ""))
	m.Status = "accepted"
	m.Message = "Ордер принят биржей; ожидается исполнение."
	if err != nil {
		m.Status = "unknown"
		m.Message = "Результат отправки не подтверждён. Проверьте статус; повторный ордер не отправляется."
		var rejected *okx.ManualCloseRejected
		if errors.As(err, &rejected) {
			m.Status = "rejected"
			m.Message = rejected.Error()
		}
	}
	save()
	return m, nil
}

func (r *Service) ManualCloseTrade(ctx context.Context, userID int64, guid, requestID uuid.UUID, fraction float64) (models.ManualClose, error) {
	user, err := r.GetUser(ctx, userID)
	if err != nil || user == nil {
		return models.ManualClose{}, models.ErrCloseNotFound
	}
	ts := user.Settings.TradingSettings
	if ts.OKXAPIKey == "" || ts.OKXAPISecret == "" || ts.OKXPassphrase == "" {
		return models.ManualClose{}, fmt.Errorf("сначала настройте API-ключи OKX")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	return executeManualClose(ctx, r.Repository, okx.NewClient(user), userID, models.ManualClose{RequestID: requestID, TradeGUID: guid, Fraction: fraction})
}

func (r *Service) ManualCloseStatus(ctx context.Context, userID int64, guid, requestID uuid.UUID) (models.ManualClose, error) {
	m, err := r.Repository.GetManualClose(ctx, userID, guid, requestID)
	if err != nil {
		return m, err
	}
	if m.Status == "rejected" {
		return m, nil
	}
	tr, err := ownedTrade(ctx, r.Repository, userID, guid)
	if err != nil {
		return m, err
	}
	user, err := r.GetUser(ctx, userID)
	if err != nil || user == nil {
		return m, models.ErrCloseNotFound
	}
	client := okx.NewClient(user)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	state, orderID, err := client.ManualCloseState(ctx, tr.InstID, strings.ReplaceAll(requestID.String(), "-", ""))
	if err != nil {
		m.Message = "Статус биржи пока не подтверждён. Повторите проверку."
		return m, nil
	}
	m.OrderID = orderID
	switch state {
	case "filled":
		m.Status = "filled"
		m.Message = "Ордер исполнен."
	case "canceled":
		m.Status = "canceled"
		m.Message = "Ордер отменён биржей; возможно частичное исполнение."
	default:
		m.Status = "accepted"
		m.Message = "Ордер ещё исполняется."
	}
	if err = r.Repository.SaveManualClose(ctx, m); err != nil {
		return m, err
	}
	if m.Status == "filled" || m.Status == "canceled" {
		remaining, posErr := client.ClosingPosition(ctx, tr.InstID, tr.Payload.PosSide)
		if posErr != nil {
			m.Message += " Остаток позиции пока не подтверждён."
			return m, nil
		}
		sess, active := r.GetSession(userID)
		if active {
			sess.ApplyManualPositionSnapshot(tr.InstID, tr.Payload.PosSide, remaining)
		}
		if remaining == 0 {
			for _, id := range []string{tr.Payload.AlgoID, tr.Payload.TPAlgoID} {
				if id != "" {
					if e := client.CancelAlgo(ctx, tr.InstID, id); e != nil {
						m.Message += " Проверьте оставшиеся SL/TP в OKX."
						break
					}
				}
			}
		}
		if !active {
			sess = &sessions.UserSession{Base: r.Base, User: user, Repo: r.Repository, Okx: client, Notifier: r.TelegramNotifier}
		}
		if err = sess.SyncClosedTrades(ctx); err != nil {
			m.Message += " История обновится после синхронизации."
		}
		if remaining == 0 {
			if e := r.Repository.TagManualClose(ctx, m); e != nil {
				m.Message += " Причина закрытия ожидает синхронизации."
			}
		}
	}
	return m, nil
}
