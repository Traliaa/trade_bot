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

	LTF string `yaml:"LTF"`
	HTF string `yaml:"HTF"`

	MinChannelPct float64 `yaml:"MinChannelPct"`
	MinBodyPct    float64 `yaml:"MinBodyPct"`

	// NEW: насколько далеко закрыться ЗА границей дончиана
	BreakoutPct float64 `yaml:"BreakoutPct"`

	HTFEmaFast int `yaml:"HTFEmaFast"`
	HTFEmaSlow int `yaml:"HTFEmaSlow"`

	MinWarmupLTF int `yaml:"MinWarmupLTF"`
	MinWarmupHTF int `yaml:"MinWarmupHTF"`

	ExpectedSymbols int           `yaml:"ExpectedSymbols"`
	ProgressEvery   time.Duration `yaml:"ProgressEvery"`
}

func NewTradingSettingsFromDefaults(userID int64, cfg *config.Config) *UserSettings {
	config := &UserSettings{
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

	if config.TradingSettings.LTF == "" {
		config.TradingSettings.LTF = "15m"
	}
	if config.TradingSettings.HTF == "" {
		config.TradingSettings.HTF = "1h"
	}

	if config.TradingSettings.DonchianPeriod <= 0 {
		config.TradingSettings.DonchianPeriod = 20
	}
	if config.TradingSettings.MinChannelPct <= 0 {
		config.TradingSettings.MinChannelPct = 0.012 // 0.8% канал
	}
	if config.TradingSettings.MinBodyPct <= 0 {
		config.TradingSettings.MinBodyPct = 0.004 // 0.3% тело
	}
	if config.TradingSettings.HTFEmaFast <= 0 {
		config.TradingSettings.HTFEmaFast = 50
	}
	if config.TradingSettings.HTFEmaSlow <= 0 {
		config.TradingSettings.HTFEmaSlow = 200
	}
	if config.TradingSettings.MinWarmupLTF <= 0 {
		config.TradingSettings.MinWarmupLTF = 20
	}
	if config.TradingSettings.MinWarmupHTF <= 0 {
		config.TradingSettings.MinWarmupHTF = 200
	}

	if config.TradingSettings.ProgressEvery <= 0 {
		config.TradingSettings.ProgressEvery = 2 * time.Minute
	}

	if config.TradingSettings.BreakoutPct <= 0 {
		// для 15m на альтах адекватный старт 0.2%–0.3%
		config.TradingSettings.BreakoutPct = 0.0025 // 0.20%
	}
	return config

}
