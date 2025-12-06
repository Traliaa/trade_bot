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

	RiskPct      float64 `yaml:"risk_pct"`       // риск от депозита, напр. 1.0 => 1% equity
	StopPct      float64 `yaml:"stop_pct"`       // расстояние до SL от цены, напр. 0.5 => 0.5%
	TakeProfitRR float64 `yaml:"take_profit_rr"` // мультипликатор TP к стопу, напр. 3.0 => TP = 3 * S
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

			RiskPct:      cfg.DefaultRiskPct,
			StopPct:      cfg.DefaultStopPct,
			TakeProfitRR: cfg.DefaultTakeProfitRR,
		},
	}

}
