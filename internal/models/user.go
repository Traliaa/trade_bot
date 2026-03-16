package models

import (
	"time"
	"trade_bot/internal/modules/config"
)

// UserSettings хранит данные пользователя
type UserSettings struct {
	ID int64 `json:"id"`

	TelegramID int64 `json:"telegram_id"` // Telegram chat/user ID

	Name            string          `json:"name"`
	Step            string          `json:"step"`
	Settings        Settings        `json:"settings"`
	Status          bool            `json:"Status"` // ✅ было ли "включено"
	Premium         bool            `json:"Premium"`
	AccountSnapshot AccountSnapshot `json:"AccountSnapshot"`
}

type AccountSnapshot struct {
	TotalEquity      float64
	AvailableBalance float64
	FrozenBalance    float64
	UnrealizedPnL    float64
	RealizedPnL      float64
	UpdatedAt        time.Time
}
type Settings struct {
	TradingSettings TradingSettings
	TrailingConfig  TrailingConfig
	FeatureFlags    FeatureConfig
	PositionGuard   PositionGuardMap `json:"position_guard"`
}

type FeatureConfig struct {
	NearTPProtectEnabled bool `json:"near_tp_protect_enabled"`
	SimulateBeforeEntry  bool `json:"simulate_before_entry"`
	DealChartEnabled     bool `json:"deal_chart_enabled"`
	AutoRecommendEnabled bool `json:"auto_recommend_enabled"`
	ProMode              bool `json:"pro_mode"`
}
type TradingSettings struct {
	// TRADE keys (у каждого юзера свои)
	OKXAPIKey     string `json:"okx_api_key"`
	OKXAPISecret  string `json:"okx_api_secret"`
	OKXPassphrase string `json:"okx_passphrase"`

	// исполнение/риск (юзер правит)
	Leverage          int `json:"leverage"`
	MaxOpenPositions  int `json:"max_open_positions"`
	MaxLongPositions  int `json:"max_long_positions"`
	MaxShortPositions int `json:"max_short_positions"`

	PositionPct float64 `json:"position_pct"` // размер позиции
	RiskPct     float64 `json:"risk_pct"`     // риск на сделку

	StopPct      float64 `json:"stop_pct"`       // расстояние SL (%)
	TakeProfitRR float64 `json:"take_profit_rr"` // TP в R

	ConfirmTimeout    time.Duration `json:"confirm_timeout"`
	CooldownPerSymbol time.Duration `json:"cooldown_per_symbol"`
}

type TrailingConfig struct {

	// --- BE / Lock ---
	BETriggerR float64 `yaml:"be_trigger_r"` // 0.6
	BEOffsetR  float64 `yaml:"be_offset_r"`  // 0.0

	LockTriggerR float64 `yaml:"lock_trigger_r"` // 0.9
	LockOffsetR  float64 `yaml:"lock_offset_r"`  // 0.3

	// --- Time stop (late) ---
	TimeStopBars        int     `yaml:"time_stop_bars"`          // 12
	TimeStopMinCurrentR float64 `yaml:"time_stop_min_current_r"` // 0.1

	// --- Early fail ---
	EarlyTimeStopBars    int     `yaml:"early_time_stop_bars"`      // 4
	EarlyTimeStopMinMFER float64 `yaml:"early_time_stop_min_mfe_r"` // 0.15

	// --- Partial ---
	PartialEnabled   bool    `yaml:"partial_enabled"`    // true
	PartialTriggerR  float64 `yaml:"partial_trigger_r"`  // 0.9
	PartialCloseFrac float64 `yaml:"partial_close_frac"` // 0.5

}

func NewTradingSettingsFromDefaults(userID int64, cfg *config.Config) *UserSettings {
	return &UserSettings{
		TelegramID: userID,
		Settings: Settings{
			TradingSettings: TradingSettings{
				Leverage:          cfg.UserDefaults.DefaultLeverage,
				MaxOpenPositions:  cfg.UserDefaults.DefaultMaxOpenPositions,
				MaxLongPositions:  cfg.UserDefaults.DefaultMaxLongPositions,
				MaxShortPositions: cfg.UserDefaults.DefaultMaxShortPositions,
				PositionPct:       cfg.UserDefaults.DefaultPositionPct,
				RiskPct:           cfg.UserDefaults.DefaultRiskPct,

				StopPct:      cfg.UserDefaults.DefaultStopPct,
				TakeProfitRR: cfg.UserDefaults.DefaultTakeProfitRR,
			},
			TrailingConfig: TrailingConfig{
				BETriggerR:          cfg.DefaultTrailing.BETriggerR,
				BEOffsetR:           cfg.DefaultTrailing.BEOffsetR,
				LockTriggerR:        cfg.DefaultTrailing.LockTriggerR,
				LockOffsetR:         cfg.DefaultTrailing.LockOffsetR,
				TimeStopBars:        cfg.DefaultTrailing.TimeStopBars,
				TimeStopMinCurrentR: cfg.DefaultTrailing.EarlyTimeStopMinMFER,

				EarlyTimeStopBars:    cfg.DefaultTrailing.EarlyTimeStopBars,
				EarlyTimeStopMinMFER: cfg.DefaultTrailing.EarlyTimeStopMinMFER,
				PartialEnabled:       cfg.DefaultTrailing.PartialEnabled,
				PartialTriggerR:      cfg.DefaultTrailing.PartialTriggerR,
				PartialCloseFrac:     cfg.DefaultTrailing.PartialCloseFrac,
			},
		},
	}
}

type UserStatus struct {
	BotRunning         bool            `json:"bot_running"`
	Account            AccountSnapshot `json:"account"`
	OpenTrades         []TradeRecord   `json:"open_trades"`
	OpenPositionsCount int             `json:"open_positions_count"`
}
