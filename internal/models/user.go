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

	StaleAfterBars    int     `json:"stale_after_bars,omitempty"`
	StaleMinMFER      float64 `json:"stale_min_mfe_r,omitempty"`
	StaleExitProfitR  float64 `json:"stale_exit_profit_r,omitempty"`
	StaleNearBER      float64 `json:"stale_near_be_r,omitempty"`
	StaleMaxAdverseR  float64 `json:"stale_max_adverse_r,omitempty"`
	StaleGraceBars    int     `json:"stale_grace_bars,omitempty"`
	StaleWorseByR     float64 `json:"stale_worse_by_r,omitempty"`
	StaleTightenToBER float64 `json:"stale_tighten_to_be_r,omitempty"`
}

func NewTradingSettingsFromDefaults(userID int64, cfg *config.Config) *UserSettings {
	config := &UserSettings{
		TelegramID: userID,
		Settings: Settings{
			TradingSettings: TradingSettings{
				Leverage:          cfg.UserDefaults.DefaultLeverage,
				MaxOpenPositions:  cfg.UserDefaults.DefaultMaxOpenPositions,
				MaxLongPositions:  cfg.UserDefaults.DefaultMaxLongPositions,
				MaxShortPositions: cfg.UserDefaults.DefaultMaxShortPositions,
				PositionPct:       cfg.UserDefaults.DefaultPositionPct,
				RiskPct:           cfg.UserDefaults.DefaultRiskPct,

				StopPct:        cfg.UserDefaults.DefaultStopPct,
				TakeProfitRR:   cfg.UserDefaults.DefaultTakeProfitRR,
				ConfirmTimeout: 60 * time.Second,
			},
			TrailingConfig: TrailingConfig{
				BETriggerR:          cfg.DefaultTrailing.BETriggerR,
				BEOffsetR:           cfg.DefaultTrailing.BEOffsetR,
				LockTriggerR:        cfg.DefaultTrailing.LockTriggerR,
				LockOffsetR:         cfg.DefaultTrailing.LockOffsetR,
				TimeStopBars:        cfg.DefaultTrailing.TimeStopBars,
				TimeStopMinCurrentR: cfg.DefaultTrailing.TimeStopMinCurrentR,

				EarlyTimeStopBars:    cfg.DefaultTrailing.EarlyTimeStopBars,
				EarlyTimeStopMinMFER: cfg.DefaultTrailing.EarlyTimeStopMinMFER,
				PartialEnabled:       cfg.DefaultTrailing.PartialEnabled,
				PartialTriggerR:      cfg.DefaultTrailing.PartialTriggerR,
				PartialCloseFrac:     cfg.DefaultTrailing.PartialCloseFrac,

				StaleAfterBars:    cfg.DefaultTrailing.Stale.AfterBars,
				StaleMinMFER:      cfg.DefaultTrailing.Stale.MinMFER,
				StaleExitProfitR:  cfg.DefaultTrailing.Stale.ExitProfitR,
				StaleNearBER:      cfg.DefaultTrailing.Stale.NearBER,
				StaleMaxAdverseR:  cfg.DefaultTrailing.Stale.MaxAdverseR,
				StaleGraceBars:    cfg.DefaultTrailing.Stale.GraceBars,
				StaleWorseByR:     cfg.DefaultTrailing.Stale.WorseByR,
				StaleTightenToBER: cfg.DefaultTrailing.Stale.TightenToBER,
			},
		},
	}

	return config
}

type UserStatus struct {
	BotRunning         bool            `json:"bot_running"`
	Account            AccountSnapshot `json:"account"`
	OpenTrades         []TradeRecord   `json:"open_trades"`
	OpenPositionsCount int             `json:"open_positions_count"`
}
type staleConfig struct {
	AfterBars    int
	MinMFER      float64
	ExitProfitR  float64
	NearBER      float64
	MaxAdverseR  float64
	GraceBars    int
	WorseByR     float64
	TightenToBER float64
}

func GetStaleConfig(cfg Settings) staleConfig {
	tc := cfg.TrailingConfig

	sc := staleConfig{
		AfterBars:    tc.StaleAfterBars,
		MinMFER:      tc.StaleMinMFER,
		ExitProfitR:  tc.StaleExitProfitR,
		NearBER:      tc.StaleNearBER,
		MaxAdverseR:  tc.StaleMaxAdverseR,
		GraceBars:    tc.StaleGraceBars,
		WorseByR:     tc.StaleWorseByR,
		TightenToBER: tc.StaleTightenToBER,
	}

	if sc.AfterBars <= 0 {
		sc.AfterBars = 16
	}
	if sc.MinMFER <= 0 {
		sc.MinMFER = 0.35
	}
	if sc.ExitProfitR <= 0 {
		sc.ExitProfitR = 0.25
	}
	if sc.NearBER == 0 {
		sc.NearBER = -0.03
	}
	if sc.MaxAdverseR == 0 {
		sc.MaxAdverseR = -0.65
	}
	if sc.GraceBars <= 0 {
		sc.GraceBars = 6
	}
	if sc.WorseByR <= 0 {
		sc.WorseByR = 0.30
	}
	if sc.TightenToBER == 0 {
		sc.TightenToBER = 0.05
	}

	return sc
}
