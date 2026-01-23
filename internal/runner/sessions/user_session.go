package sessions

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	okx_client "trade_bot/internal/modules/okx_client/service"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramNotifier interface {
	SendF(ctx context.Context, chatID int64, format string, args ...any) (tgbot.Message, error)
	Send(ctx context.Context, chatID int64, msg string) (tgbot.Message, error)
	Confirm(ctx context.Context, chatID int64, prompt string, timeout time.Duration) bool
}

type UserSession struct {
	Ctx    context.Context
	Cancel context.CancelFunc

	mu         sync.Mutex   // можно оставить для Pending/Cooldown
	PosMu      sync.RWMutex // 🔒 Positions (trail state)
	PosCacheMu sync.RWMutex // 🔒 PositionsCache (OKX cache)

	//ид пользователя
	UserID int64
	//настройки пользователя
	Settings *models.UserSettings

	// трейлинг состояние
	Positions map[string]*models.PositionTrailState // key = instId:posSide

	// кеш позиций
	PositionsCache map[models.PosKey]models.CachedPos
	PosCacheAt     time.Time // когда последний раз обновляли с OKX

	//сенлдер в телеграм
	Notifier TelegramNotifier
	//клиент биржи
	Okx *okx_client.Client

	Queue       chan models.Signal
	Pending     map[string]bool
	CooldownTil map[string]time.Time

	msgMu     sync.Mutex
	LastMsgAt map[string]time.Time // key -> time
}

// OpenPositionWithTpSl открывает рыночный ордер и пытается поставить TP/SL.
// Возвращает orderID рыночного ордера (если успешно) или ошибку.
func (s *UserSession) OpenPositionWithTpSl(
	ctx context.Context,
	sig models.Signal,
	params *models.TradeParams,
) (*models.OpenResult, error) {

	// 1. Маппим сторону в OKX side/openType
	openType := 1 // 1 = open long/short
	var sideInt int
	switch strings.ToUpper(params.Direction) {
	case "BUY":
		sideInt = 1 // open long
	case "SELL":
		sideInt = 3 // open short
	default:
		return nil, fmt.Errorf("unknown direction: %q", params.Direction)
	}

	ts := s.Settings.Settings.TradingSettings

	fmt.Printf(
		"[CREDS CHECK INSIDE calcSizeByRisk] chat=%d keyLen=%d secretLen=%d passLen=%d",
		s.UserID,
		len(ts.OKXAPIKey),
		len(ts.OKXAPISecret),
		len(ts.OKXPassphrase),
	)
	// 2. Открываем рыночный ордер
	orderID, err := s.Okx.PlaceMarket(
		ctx,
		sig.InstID,
		params.Size,
		sideInt,
		params.Leverage,
		openType,
	)
	if err != nil {
		return nil, fmt.Errorf("PlaceMarket: %w", err)
	}

	// 3. TP/SL (order-algo)
	posSide := "long"
	if strings.EqualFold(params.Direction, "SELL") {
		posSide = "short"
	}

	// debug для себя
	s.Notifier.SendF(ctx, s.UserID,
		"[%s] DEBUG entry=%.6f SL=%.6f TP=%.6f 1R=%.6f RR=%.2f risk=%.2f%% size=%.4f (%s)",
		sig.InstID,
		params.Entry, params.SL, params.TP, params.RiskDist,
		params.RR, params.RiskPct, params.Size,
		sig.Reason,
	)

	// 1) Stop-loss
	slAlgoId, err := s.Okx.PlaceSingleAlgo(ctx, sig.InstID, posSide, params.Size, params.SL, false)
	if err != nil {
		s.Notifier.SendF(ctx, s.UserID,
			"⚠️ [%s] TP/SL не выставлены на OKX: %v", sig.InstID, err)
	}

	// 2) Take-profit
	tpAlgoId, err := s.Okx.PlaceSingleAlgo(ctx, sig.InstID, posSide, params.Size, params.TP, true)
	if err != nil {
		s.Notifier.SendF(ctx, s.UserID,
			"⚠️ [%s] TP/SL не выставлены на OKX: %v", sig.InstID, err)

	}

	// 4. Финальное сообщение об успешном входе
	s.Notifier.SendF(ctx,
		s.UserID,
		"✅ [%s] Вход подтверждён | OPEN %-4s @ %.4f | SL=%.4f TP=%.4f lev=%dx size=%.4f | strategy=%s (orderId=%s)",
		sig.InstID,
		params.Direction,
		params.Entry,
		params.SL,
		params.TP,
		params.Leverage,
		params.Size,
		sig.Strategy,
		orderID,
	)

	return &models.OpenResult{PosSide: posSide, SLAlgoID: slAlgoId, TPAlgoID: tpAlgoId, Entry: params.Entry}, nil
}
func (s *UserSession) Status(ctx context.Context) ([]models.OpenPosition, error) {
	// просто прокидываем в OKX-клиент, который уже сконфигурен под этого юзера
	positions, err := s.Okx.OpenPositions(ctx)
	if err != nil {
		return nil, err
	}
	return positions, nil
}

func (s *UserSession) upsertTrailState(st *models.PositionTrailState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Positions[helper.TrailKey(st.InstID, st.PosSide)] = st
}

func improvesEnough(oldSL, newSL float64, posSide string, min float64) bool {
	if posSide == "long" {
		return newSL-oldSL >= min
	}
	return oldSL-newSL >= min
}

func (s *UserSession) canSend(key string, every time.Duration) bool {
	s.msgMu.Lock()
	defer s.msgMu.Unlock()

	now := time.Now()
	if t, ok := s.LastMsgAt[key]; ok && now.Sub(t) < every {
		return false
	}
	s.LastMsgAt[key] = now
	return true
}
