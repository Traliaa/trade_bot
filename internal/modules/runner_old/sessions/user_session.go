package sessions

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	okx_client "trade_bot/internal/modules/okx_client/service"
	"trade_bot/internal/modules/repository/pg"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramNotifier interface {
	SendF(ctx context.Context, chatID int64, format string, args ...any) (tgbot.Message, error)
	Send(ctx context.Context, chatID int64, msg string) (tgbot.Message, error)
}

type UserSession struct {
	base.Base
	Ctx      context.Context
	Cancel   context.CancelFunc
	settings atomic.Value // stores models.Settings
	Repo     *pg.User     // ✅ добавили

	//mu         sync.Mutex   // можно оставить для Pending/Cooldown
	//PosMu      sync.RWMutex // 🔒 Positions (trail state)
	//PosCacheMu sync.RWMutex // 🔒 PositionsCache (OKX cache)

	//настройки пользователя
	User *models.UserSettings

	TrailMu     sync.RWMutex
	TrailStates map[models.PosKey]*models.PositionTrailState

	ExchangeMu          sync.RWMutex
	ExchangePositions   map[models.PosKey]models.CachedPos
	ExchangePositionsAt time.Time
	//// трейлинг состояние
	//Positions map[string]*models.PositionTrailState // key = instId:posSide
	//
	//// кеш позиций
	//PositionsCache map[models.PosKey]models.CachedPos
	//PosCacheAt     time.Time // когда последний раз обновляли с OKX

	//сенлдер в телеграм
	Notifier TelegramNotifier
	//клиент биржи
	okxMu sync.RWMutex
	Okx   *okx_client.Client

	msgMu     sync.Mutex
	LastMsgAt map[string]time.Time // key -> time

	WG sync.WaitGroup

	stopCh chan struct{}
}

// OpenPositionWithTpSl открывает рыночный ордер и пытается поставить TP/SL.
// Возвращает orderID рыночного ордера (если успешно) или ошибку.
func (s *UserSession) OpenPositionWithTpSl(
	ctx context.Context,
	sig models.Signal,
	params *models.TradeParams,
) (*models.OpenResult, error) {
	cfg := s.SettingsSnapshot()
	ts := cfg.TradingSettings
	//tr := cfg.TrailingConfig
	//ff := cfg.FeatureFlags

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

	fmt.Printf(
		"[CREDS CHECK INSIDE calcSizeByRisk] chat=%d keyLen=%d secretLen=%d passLen=%d",
		s.User.TelegramID,
		len(ts.OKXAPIKey),
		len(ts.OKXAPISecret),
		len(ts.OKXPassphrase),
	)
	// 2. Открываем рыночный ордер
	OrderID, err := s.Okx.PlaceMarket(
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

	// 1) Stop-loss
	slAlgoId, err := s.Okx.PlaceSingleAlgo(ctx, sig.InstID, posSide, params.Size, params.SL, false)
	if err != nil {
		s.Notifier.SendF(ctx, s.User.TelegramID,
			"⚠️ [%s] TP/SL не выставлены на OKX: %v", sig.InstID, err)
	}

	// 2) Take-profit
	tpAlgoId, err := s.Okx.PlaceSingleAlgo(ctx, sig.InstID, posSide, params.Size, params.TP, true)
	if err != nil {
		s.Notifier.SendF(ctx, s.User.TelegramID,
			"⚠️ [%s] TP/SL не выставлены на OKX: %v", sig.InstID, err)

	}

	return &models.OpenResult{PosSide: posSide, SLAlgoID: slAlgoId, TPAlgoID: tpAlgoId, Entry: params.Entry, OrderID: OrderID}, nil
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
	s.TrailMu.Lock()
	defer s.TrailMu.Unlock()
	s.TrailStates[helper.TrailKey(st.InstID, st.PosSide)] = st
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

func (s *UserSession) InitSettings(cfg models.Settings) {
	s.settings.Store(cfg)
}

func (s *UserSession) SettingsSnapshot() models.Settings {
	v := s.settings.Load()
	if v == nil {
		return models.Settings{}
	}
	return v.(models.Settings)
}

func (s *UserSession) UpdateSettings(cfg models.Settings) {
	s.settings.Store(cfg)
}

func (s *UserSession) UpdateOKXClient(user *models.UserSettings) {
	s.okxMu.Lock()
	defer s.okxMu.Unlock()
	s.Okx = okx_client.NewClient(user)
}

func (s *UserSession) OkxClient() *okx_client.Client {
	s.okxMu.RLock()
	defer s.okxMu.RUnlock()
	return s.Okx
}
