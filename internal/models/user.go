package models

import (
	"time"
	"trade_bot/internal/modules/config"
)

// UserSettings хранит данные пользователя
type UserSettings struct {
	ID int64 `json:"id"`

	UserID int64 `json:"user_id"` // Telegram chat/user ID

	Name            string          `json:"name"`
	Step            string          `json:"step"`
	TradingSettings TradingSettings `json:"settings"`
}

type TradingSettings struct {

	// OKX
	OKXAPIKey     string `json:"okx_api_key"`
	OKXAPISecret  string `json:"okx_api_secret"`
	OKXPassphrase string `json:"okx_passphrase"`

	// Стратегия
	Timeframe     string  `json:"timeframe"`
	EMAShort      int     `json:"ema_short"`
	EMALong       int     `json:"ema_long"`
	RSIPeriod     int     `json:"rsi_period"`
	RSIOverbought float64 `json:"rsi_overbought"`
	RSIOSold      float64 `json:"rsi_oversold"`

	PositionPct      float64 `json:"position_pct"`
	Leverage         int     `json:"leverage"`
	MaxOpenPositions int     `json:"max_open_positions"`

	// Очередь подтверждений
	ConfirmRequired    bool          `json:"confirm_required"`
	CooldownPerSymbol  time.Duration `json:"cooldown_per_symbol"`
	ConfirmTimeout     time.Duration `json:"confirm_timeout"`
	AutoOnTimeout      string        `json:"auto_on_timeout"`
	ConfirmQueueMax    int           `json:"confirm_queue_max"`
	WatchTopN          int           `json:"watch_top_n"`
	ConfirmQueuePolicy string        `json:"confirm_queue_policy"`

	// Сколько от депозита мы готовы потерять по СТОПУ, а не по ликвидации
	RiskPct      float64 `yaml:"risk_pct"`       // например 1.0 => 1% equity
	TakeProfitRR float64 `yaml:"take_profit_rr"` // например 3.0 => TP = 3R

	DonchianPeriod int `yaml:"donchain_period"`   // период канала, N свечей (обычно 20)
	TrendEmaPeriod int `yaml:"trena_ema__period"` // EMA для фильтра тренда (обычно 50)

	Strategy StrategyType `json:"strategy"` // "emarsi" или "donchian"
	StopPct  float64      `yaml:"stop_pct"`

	// --- трейлинг / фиксация ---
	TrailEnabled bool

	BETriggerR float64 // 0.6
	BEOffsetR  float64 // 0.0

	LockTriggerR float64 // 0.9
	LockOffsetR  float64 // 0.3

	TimeStopBars    int     // 12 (15m свечей)
	TimeStopMinMFER float64 // 0.3

	// --- 🔥 ЧАСТИЧНАЯ ФИКСАЦИЯ ---
	PartialEnabled   bool    // true
	PartialTriggerR  float64 // 0.9
	PartialCloseFrac float64 // 0.5 (50%)
}

func NewTradingSettingsFromDefaults(userID int64, cfg *config.Config) *UserSettings {
	return &UserSettings{
		UserID: userID,
		TradingSettings: TradingSettings{

			Timeframe:     cfg.DefaultTimeframe,
			EMAShort:      cfg.DefaultEMAShort,
			EMALong:       cfg.DefaultEMALong,
			RSIPeriod:     cfg.DefaultRSIPeriod,
			RSIOverbought: cfg.DefaultRSIOverbought,
			RSIOSold:      cfg.DefaultRSIOSold,

			PositionPct:      cfg.DefaultPositionPct,
			Leverage:         cfg.DefaultLeverage,
			MaxOpenPositions: cfg.DefaultMaxOpenPositions,

			ConfirmRequired:   cfg.DefaultConfirmRequired,
			CooldownPerSymbol: cfg.DefaultCooldownPerSymbol,
			ConfirmTimeout:    cfg.DefaultConfirmTimeout,
			AutoOnTimeout:     cfg.DefaultAutoOnTimeout,
			WatchTopN:         cfg.DefaultWatchTopN,

			RiskPct:        cfg.DefaultRiskPct,
			TakeProfitRR:   cfg.DefaultTakeProfitRR,
			DonchianPeriod: cfg.DefaultDonchianPeriod,
			TrendEmaPeriod: cfg.DefaultTrendEmaPeriod,
			Strategy:       StrategyType(cfg.DefaultStrategy),
			StopPct:        cfg.StopPct,
			TrailEnabled:   true,
			BETriggerR:     0.6,
			BEOffsetR:      0,
			LockTriggerR:   0.9,
			LockOffsetR:    0.3,

			TimeStopBars:    12,
			TimeStopMinMFER: 0.3,

			PartialEnabled:   true,
			PartialTriggerR:  0.9,
			PartialCloseFrac: 0.5,
		},
	}

}
