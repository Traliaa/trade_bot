package models

import (
	"time"
	"trade_bot/internal/modules/config"
)

// UserSettings хранит данные пользователя
type UserSettings struct {
	ID int64 `json:"id"`

	UserID int64 `json:"user_id"` // Telegram chat/user ID

	Name     string   `json:"name"`
	Step     string   `json:"step"`
	Settings Settings `json:"settings"`
}
type Settings struct {
	TradingSettings TradingSettings
	TrailingConfig  TrailingConfig
	FeatureFlags    FeatureFlags
}

type FeatureFlags struct {
	// защиты/автоматизации
	NearTPProtectEnabled bool `json:"near_tp_protect_enabled"`

	// UX / качество жизни
	TradeSimulationEnabled bool `json:"trade_simulation_enabled"` // 🧪 симуляция сделки перед входом
	DealChartEnabled       bool `json:"deal_chart_enabled"`       // 📉 график сделки в TG
	AutoRecommendEnabled   bool `json:"auto_recommend_enabled"`   // 🤖 авто-рекомендации настроек
	ProModeEnabled         bool `json:"pro_mode_enabled"`         // 💎 PRO режим (показывать расширенные пункты)
}
type TradingSettings struct {
	// TRADE keys (у каждого юзера свои)
	OKXAPIKey     string `json:"okx_api_key"`
	OKXAPISecret  string `json:"okx_api_secret"`
	OKXPassphrase string `json:"okx_passphrase"`

	// исполнение/риск (юзер правит)
	Leverage         int     `json:"leverage"`
	MaxOpenPositions int     `json:"max_open_positions"`
	PositionPct      float64 `json:"position_pct"` // размер позиции
	RiskPct          float64 `json:"risk_pct"`     // риск на сделку

	StopPct      float64 `json:"stop_pct"`       // расстояние SL (%)
	TakeProfitRR float64 `json:"take_profit_rr"` // TP в R

	// подтверждения
	ConfirmRequired   bool          `json:"confirm_required"`
	ConfirmTimeout    time.Duration `json:"confirm_timeout"`
	CooldownPerSymbol time.Duration `json:"cooldown_per_symbol"`
}

type TrailingConfig struct {
	// --- BE / Lock ---
	BETriggerR float64 `yaml:"be_trigger_r"` // 0.6
	BEOffsetR  float64 `yaml:"be_offset_r"`  // 0.0

	LockTriggerR float64 `yaml:"lock_trigger_r"` // 0.9
	LockOffsetR  float64 `yaml:"lock_offset_r"`  // 0.3

	// --- Time stop ---
	TimeStopBars    int     `yaml:"time_stop_bars"`      // 12
	TimeStopMinMFER float64 `yaml:"time_stop_min_mfe_r"` // 0.3

	// --- Partial ---
	PartialEnabled   bool    `yaml:"partial_enabled"`    // true
	PartialTriggerR  float64 `yaml:"partial_trigger_r"`  // 0.9
	PartialCloseFrac float64 `yaml:"partial_close_frac"` // 0.5
}

func NewTradingSettingsFromDefaults(userID int64, cfg *config.Config) *UserSettings {
	return &UserSettings{
		UserID: userID,
		Settings: Settings{
			TradingSettings: TradingSettings{
				Leverage:         cfg.UserDefaults.DefaultLeverage,
				MaxOpenPositions: cfg.UserDefaults.DefaultMaxOpenPositions,
				PositionPct:      cfg.UserDefaults.DefaultPositionPct,
				RiskPct:          cfg.UserDefaults.DefaultRiskPct,

				StopPct:      cfg.UserDefaults.DefaultStopPct,
				TakeProfitRR: cfg.UserDefaults.DefaultTakeProfitRR,

				ConfirmRequired:   cfg.UserDefaults.DefaultConfirmRequired,
				CooldownPerSymbol: cfg.UserDefaults.DefaultCooldownPerSymbol,
				ConfirmTimeout:    cfg.UserDefaults.DefaultConfirmTimeout,
			},
			TrailingConfig: TrailingConfig{
				BETriggerR:       cfg.DefaultTrailing.BETriggerR,
				BEOffsetR:        cfg.DefaultTrailing.BEOffsetR,
				LockTriggerR:     cfg.DefaultTrailing.LockTriggerR,
				LockOffsetR:      cfg.DefaultTrailing.LockOffsetR,
				TimeStopBars:     cfg.DefaultTrailing.TimeStopBars,
				TimeStopMinMFER:  cfg.DefaultTrailing.TimeStopMinMFER,
				PartialEnabled:   cfg.DefaultTrailing.PartialEnabled,
				PartialTriggerR:  cfg.DefaultTrailing.PartialTriggerR,
				PartialCloseFrac: cfg.DefaultTrailing.PartialCloseFrac,
			},
		},
	}
}
