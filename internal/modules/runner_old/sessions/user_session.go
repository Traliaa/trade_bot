package sessions

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	okx_client "trade_bot/internal/modules/okx_client/service"
	"trade_bot/internal/modules/repository/pg"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

type TelegramNotifier interface {
	SendF(ctx context.Context, chatID int64, format string, args ...any) (tgbot.Message, error)
	Send(ctx context.Context, chatID int64, msg string) (tgbot.Message, error)
}

type UserSession struct {
	base.Base
	Ctx    context.Context
	Cancel context.CancelFunc

	Config   *config.Config
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

	// V3PartialCheck опциональный коллбэк для проверки, зафиксировала ли V3-стратегия частичное закрытие.
	// Устанавливается runner-сервисом.
	V3PartialCheck func(instID string) bool
}

func validateFinalOrderSize(finalSz float64, calc *models.SizeCalcResult) error {
	if calc == nil {
		return fmt.Errorf("nil size calc result")
	}
	if finalSz <= 0 || math.IsNaN(finalSz) || math.IsInf(finalSz, 0) {
		return fmt.Errorf("invalid final size: %.10f", finalSz)
	}

	// нельзя больше рассчитанного нормализованного размера
	if finalSz > calc.NormalizedSz+1e-9 {
		return fmt.Errorf(
			"final size exceeds normalized size: final=%.8f normalized=%.8f",
			finalSz, calc.NormalizedSz,
		)
	}

	// повторная проверка кратности шагу
	steps := finalSz / calc.LotSz
	if math.Abs(steps-math.Round(steps)) > 1e-8 {
		return fmt.Errorf(
			"final size is not aligned to lotSz: final=%.8f lotSz=%.8f",
			finalSz, calc.LotSz,
		)
	}

	if finalSz < calc.MinSz {
		return fmt.Errorf(
			"final size below minSz: final=%.8f minSz=%.8f",
			finalSz, calc.MinSz,
		)
	}

	if calc.MaxMktSz > 0 && finalSz > calc.MaxMktSz {
		return fmt.Errorf(
			"final size exceeds maxMktSz: final=%.8f max=%.8f",
			finalSz, calc.MaxMktSz,
		)
	}

	return nil
}

// OpenPositionWithTpSl открывает рыночный ордер и пытается поставить TP/SL.
// Возвращает orderID рыночного ордера (если успешно) или ошибку.
// OpenPositionWithTpSl открывает рыночный ордер и пытается поставить TP/SL.
// Возвращает orderID рыночного ордера (если успешно) или ошибку.
func (s *UserSession) OpenPositionWithTpSl(
	ctx context.Context,
	sig models.Signal,
	params *models.TradeParams,
) (*models.OpenResult, error) {
	if params == nil {
		return nil, fmt.Errorf("params is nil")
	}
	if params.SizeMeta == nil {
		return nil, fmt.Errorf("params.SizeMeta is nil")
	}

	if err := validateFinalOrderSize(params.Size, params.SizeMeta); err != nil {
		return nil, fmt.Errorf("final size validation failed: %w", err)
	}

	posSide := "long"
	switch strings.ToUpper(params.Direction) {
	case "BUY":
		posSide = "long"
	case "SELL":
		posSide = "short"
	default:
		return nil, fmt.Errorf("unknown direction: %q", params.Direction)
	}

	// Антидубль: не открываем новую сделку, если уже есть открытая по тому же инструменту и направлению.
	openTrades, err := s.Repo.ListOpenTrades(ctx, s.User.TelegramID)
	if err != nil {
		return nil, fmt.Errorf("list open trades: %w", err)
	}
	for _, tr := range openTrades {
		if tr.InstID == sig.InstID && strings.EqualFold(tr.Payload.PosSide, posSide) {
			return nil, fmt.Errorf(
				"open trade already exists: inst=%s pos_side=%s guid=%s entry_at=%s",
				tr.InstID,
				tr.Payload.PosSide,
				tr.GUID,
				tr.EntryAt.UTC().Format(time.RFC3339),
			)
		}
	}

	cfg := s.SettingsSnapshot()
	ts := cfg.TradingSettings

	openType := 1
	var sideInt int

	switch strings.ToUpper(params.Direction) {
	case "BUY":
		sideInt = 1
	case "SELL":
		sideInt = 3
	default:
		return nil, fmt.Errorf("unknown direction: %q", params.Direction)
	}

	log.Printf(
		"[OPEN CHECK] inst=%s dir=%s posSide=%s size=%.8f normalized=%.8f riskUSDT=%.8f entry=%.8f sl=%.8f tp=%.8f lev=%d chat=%d key=%t secret=%t pass=%t",
		sig.InstID,
		params.Direction,
		posSide,
		params.Size,
		params.SizeMeta.NormalizedSz,
		params.SizeMeta.RiskUSDT,
		params.Entry,
		params.SL,
		params.TP,
		params.Leverage,
		s.User.TelegramID,
		ts.OKXAPIKey != "",
		ts.OKXAPISecret != "",
		ts.OKXPassphrase != "",
	)

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

	entryPrice := params.Entry
	entryAt := time.Now().UTC()
	entryFills, fillErr := s.Okx.WaitOrderFills(ctx, sig.InstID, orderID, params.Size, 3*time.Second)
	if fillErr != nil {
		s.Logger.Warn("entry fill reconciliation failed; using signal price",
			zap.Error(fillErr),
			zap.String("instId", sig.InstID),
			zap.String("orderID", orderID),
		)
	} else if fillPrice, fillSize := fillVWAP(entryFills); fillPrice > 0 && fillSize > 0 {
		entryPrice = fillPrice
		entryAt = latestFillTime(entryFills, entryAt)
		params.Entry = fillPrice
		params.Size = fillSize
		params.RiskDist = math.Abs(fillPrice - params.SL)
		if params.RiskDist > 0 {
			params.RR = math.Abs(params.TP-fillPrice) / params.RiskDist
		}
	}

	slAlgoId, err := s.Okx.PlaceSingleAlgo(ctx, sig.InstID, posSide, params.Size, params.SL, false)
	if err != nil {
		s.Notifier.SendF(ctx, s.User.TelegramID,
			"⚠️ [%s] SL не выставлен на OKX: %v", sig.InstID, err)
		if _, closeErr := s.Okx.CloseMarket(ctx, sig.InstID, posSide, params.Size); closeErr != nil {
			return nil, fmt.Errorf("place SL: %w; emergency close: %v", err, closeErr)
		}
		return nil, fmt.Errorf("place SL: %w; position closed immediately", err)
	}

	tpAlgoId, err := s.Okx.PlaceSingleAlgo(ctx, sig.InstID, posSide, params.Size, params.TP, true)
	if err != nil {
		s.Notifier.SendF(ctx, s.User.TelegramID,
			"⚠️ [%s] TP не выставлен на OKX: %v", sig.InstID, err)
	}

	return &models.OpenResult{
		PosSide:  posSide,
		SLAlgoID: slAlgoId,
		TPAlgoID: tpAlgoId,
		Entry:    entryPrice,
		EntryAt:  entryAt,
		OrderID:  orderID,
		Fills:    entryFills,
	}, nil
}

func latestFillTime(fills []models.TradeFill, fallback time.Time) time.Time {
	var latest time.Time
	for _, fill := range fills {
		if !fill.FillTime.IsZero() && (latest.IsZero() || fill.FillTime.After(latest)) {
			latest = fill.FillTime
		}
	}
	if latest.IsZero() {
		return fallback
	}
	return latest
}

func fillVWAP(fills []models.TradeFill) (price float64, size float64) {
	var notional float64
	for _, fill := range fills {
		if fill.FillPx <= 0 || fill.FillSz <= 0 {
			continue
		}
		notional += fill.FillPx * fill.FillSz
		size += fill.FillSz
	}
	if size <= 0 {
		return 0, 0
	}
	return notional / size, size
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
