package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v2"
)

const (
	configFilePathENV     = "CONFIG_FILE"
	tokenTelegramENV      = "TELEGRAM_TOKEN"
	databaseDSN           = "DATABASE_DSN"
	ServiceTelegramChatID = "CHAT_ID"
	OKXWSAPIKey           = "OKXWS_API_KEY"
	OKXWSAPISecret        = "OKXWS_API_SECRET"
	OKXWSAPIPassphrase    = "OKXWS_API_PASS"
)

type Config struct {
	Telegram struct {
		Token string `yaml:"token"`
	} `yaml:"telegram"`

	DB string `yaml:"db_dsn"`

	Service struct {
		Host       string `yaml:"host"`
		PublicPort int    `yaml:"public_port"`
		AdminPort  int    `yaml:"admin_port"`
		Workers    int    `yaml:"workers"`
	} `yaml:"service"`

	// сервисный чат (куда слать сервис-алерты)
	ServiceTelegramChatID int `yaml:"service_telegram_chat_id"`

	// ✅ OKX КЛЮЧИ СЕРВИСА (только для WS / watchlist / warmup)
	OKXWS struct {
		APIKey     string `yaml:"api_key"`
		APISecret  string `yaml:"api_secret"`
		Passphrase string `yaml:"passphrase"`
	} `yaml:"okx_ws"`

	// ✅ Стратегия (общая для сервиса, одинаковая для всех юзеров)
	Strategy StrategyConfig `yaml:"strategy"`

	// ✅ Дефолты при создании нового юзера (только initial values)
	UserDefaults    UserDefaultsConfig     `yaml:"user_defaults"`
	DefaultTrailing TrailingDefaultsConfig `yaml:"default_trailing"`
}

type StrategyConfig struct {
	Name string `yaml:"name"`

	LTF string `yaml:"ltf"`
	HTF string `yaml:"htf"`

	MinConfirmScore         int     `yaml:"min_confirm_score"`
	AddonMinScore           int     `yaml:"addon_min_score"`
	MaxAdds                 int     `yaml:"max_adds"`
	AddonCooldownBars       int     `yaml:"addon_cooldown_bars"`
	RecoveryWatchBars       int     `yaml:"recovery_watch_bars"`
	CompressionThresholdPct float64 `yaml:"compression_threshold_pct"`
	MaxDistanceFromLevelPct float64 `yaml:"max_distance_from_level_pct"`

	StrongCloseMin        float64 `yaml:"strong_close_min"`
	StrongCloseMax        float64 `yaml:"strong_close_max"`
	ImpulseBodyMinPct     float64 `yaml:"impulse_body_min_pct"`
	RetestTolerancePct    float64 `yaml:"retest_tolerance_pct"`
	ReclaimTolerancePct   float64 `yaml:"reclaim_tolerance_pct"`
	StructureLookbackBars int     `yaml:"structure_lookback_bars"`

	DonchianPeriod int     `yaml:"donchian_period"`
	MinChannelPct  float64 `yaml:"min_channel_pct"`
	MinBodyPct     float64 `yaml:"min_body_pct"`
	BreakoutPct    float64 `yaml:"breakout_pct"`

	HTFEmaFast int `yaml:"htf_ema_fast"`
	HTFEmaSlow int `yaml:"htf_ema_slow"`

	MinWarmupLTF int `yaml:"min_warmup_ltf"`
	MinWarmupHTF int `yaml:"min_warmup_htf"`

	ExpectedSymbols int           `yaml:"expected_symbols"`
	ProgressEvery   time.Duration `yaml:"progress_every"`

	WatchTopN int `yaml:"watch_top_n"`

	Symbols []string `yaml:"symbols"`

	TuneMode            int     `yaml:"tune_mode"`
	MinRetestDepthPct   float64 `yaml:"min_retest_depth_pct"`
	MaxBreakoutBodyPct  float64 `yaml:"max_breakout_body_pct"`
	MaxBreakoutRangePct float64 `yaml:"max_breakout_range_pct"`
	MinConfirmBodyFrac  float64 `yaml:"min_confirm_body_frac"`
	MaxConfirmWickFrac  float64 `yaml:"max_confirm_wick_frac"`

	MaxRetestBars     int     `yaml:"max_retest_bars"`
	MaxRetestStretchR float64 `yaml:"max_retest_stretch_r"`
	WorkingMovePct    float64 `yaml:"working_move_pct"`
	RecoveryMovePct   float64 `yaml:"recovery_move_pct"`

	MinRR              float64 `yaml:"min_rr"`
	SLBufferPct        float64 `yaml:"sl_buffer_pct"`
	SwingLookbackBars  int     `yaml:"swing_lookback_bars"`
	TargetLookbackBars int     `yaml:"target_lookback_bars"`

	UsePercentFallback bool    `yaml:"use_percent_fallback"`
	FallbackStopPct    float64 `yaml:"fallback_stop_pct"`

	ATRPeriod   int     `mapstructure:"atr_period"`
	ATRStopMult float64 `mapstructure:"atr_stop_mult"`
	UseATRGuard bool    `mapstructure:"use_atr_guard"`
}

type UserDefaultsConfig struct {
	// стартовые дефолты для нового юзера
	DefaultLeverage          int     `yaml:"default_leverage"`
	DefaultMaxOpenPositions  int     `yaml:"default_max_open_positions"`
	DefaultMaxLongPositions  int     `yaml:"default_max_long_positions"`
	DefaultMaxShortPositions int     `yaml:"default_max_short_positions"`
	DefaultPositionPct       float64 `yaml:"default_position_pct"`
	DefaultRiskPct           float64 `yaml:"default_risk_pct"`
	DefaultStopPct           float64 `yaml:"default_stop_pct"`
	DefaultTakeProfitRR      float64 `yaml:"default_take_profit_rr"`
}

type TrailingDefaultsConfig struct {

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

func NewConfig() (*Config, error) {
	cfg := &Config{}

	// --- читаем yaml ---
	configFileName := os.Getenv(configFilePathENV)
	if configFileName == "" {
		configFileName = "values_local.yaml"
	}

	file, err := os.Open("configs/" + configFileName)
	if err != nil {
		log.Printf("failed to open config file: %v", err)
		return nil, err
	}
	defer file.Close()

	dec := yaml.NewDecoder(file)
	if err := dec.Decode(cfg); err != nil {
		log.Printf("failed to decode config file: %v", err)
		return nil, err
	}

	// --- env overrides ---
	if v := os.Getenv(tokenTelegramENV); v != "" {
		cfg.Telegram.Token = v
	}
	if v := os.Getenv(databaseDSN); v != "" {
		cfg.DB = v
	}

	cfg.ServiceTelegramChatID = intFromEnv(ServiceTelegramChatID, cfg.ServiceTelegramChatID)

	// WS keys (сервисные)
	if v := os.Getenv(OKXWSAPIKey); v != "" {
		cfg.OKXWS.APIKey = v
	}
	if v := os.Getenv(OKXWSAPISecret); v != "" {
		cfg.OKXWS.APISecret = v
	}
	if v := os.Getenv(OKXWSAPIPassphrase); v != "" {
		cfg.OKXWS.Passphrase = v
	}

	// --- validate ---
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	cfg.Strategy.Name = "donchian_v3_smart"
	cfg.Strategy.ApplyV3Defaults()
	return cfg, nil
}
func intFromEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func validateConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	if cfg.Telegram.Token == "" {
		log.Printf("WARN: telegram.token is empty (env %s or yaml telegram.token)", tokenTelegramENV)
	}

	if cfg.Service.Host == "" {
		return fmt.Errorf("service.host is required")
	}
	if cfg.Service.PublicPort <= 0 {
		return fmt.Errorf("service.public_port must be > 0")
	}
	if cfg.Service.AdminPort <= 0 {
		return fmt.Errorf("service.admin_port must be > 0")
	}
	if cfg.Service.Workers <= 0 {
		return fmt.Errorf("service.workers must be > 0")
	}

	if cfg.Strategy.LTF == "" {
		return fmt.Errorf("strategy.ltf is required")
	}
	if cfg.Strategy.HTF == "" {
		return fmt.Errorf("strategy.htf is required")
	}
	if cfg.Strategy.DonchianPeriod <= 0 {
		return fmt.Errorf("strategy.donchian_period must be > 0")
	}
	if cfg.Strategy.MinWarmupLTF <= 0 {
		return fmt.Errorf("strategy.min_warmup_ltf must be > 0")
	}
	if cfg.Strategy.MinWarmupHTF <= 0 {
		return fmt.Errorf("strategy.min_warmup_htf must be > 0")
	}
	if cfg.Strategy.ExpectedSymbols < 0 {
		return fmt.Errorf("strategy.expected_symbols must be >= 0")
	}
	if cfg.Strategy.WatchTopN < 0 {
		return fmt.Errorf("strategy.watch_top_n must be >= 0")
	}

	if cfg.UserDefaults.DefaultLeverage <= 0 {
		return fmt.Errorf("user_defaults.default_leverage must be > 0")
	}
	if cfg.UserDefaults.DefaultMaxOpenPositions <= 0 {
		return fmt.Errorf("user_defaults.default_max_open_positions must be > 0")
	}
	if cfg.UserDefaults.DefaultPositionPct <= 0 {
		return fmt.Errorf("user_defaults.default_position_pct must be > 0")
	}
	if cfg.UserDefaults.DefaultRiskPct <= 0 {
		return fmt.Errorf("user_defaults.default_risk_pct must be > 0")
	}
	if cfg.UserDefaults.DefaultTakeProfitRR <= 0 {
		return fmt.Errorf("user_defaults.default_take_profit_rr must be > 0")
	}

	if cfg.DefaultTrailing.BETriggerR < 0 {
		return fmt.Errorf("default_trailing.be_trigger_r must be >= 0")
	}
	if cfg.DefaultTrailing.LockTriggerR < 0 {
		return fmt.Errorf("default_trailing.lock_trigger_r must be >= 0")
	}
	if cfg.DefaultTrailing.TimeStopBars < 0 {
		return fmt.Errorf("default_trailing.time_stop_bars must be >= 0")
	}
	if cfg.DefaultTrailing.EarlyTimeStopBars < 0 {
		return fmt.Errorf("default_trailing.early_time_stop_bars must be >= 0")
	}
	if cfg.DefaultTrailing.PartialCloseFrac < 0 || cfg.DefaultTrailing.PartialCloseFrac > 1 {
		return fmt.Errorf("default_trailing.partial_close_frac must be in [0..1]")
	}

	return nil
}
func (c *StrategyConfig) ApplyV3Defaults() {
	if c.MinConfirmScore <= 0 {
		c.MinConfirmScore = 4
	}
	if c.AddonMinScore <= 0 {
		c.AddonMinScore = 4
	}
	if c.MaxAdds <= 0 {
		c.MaxAdds = 1
	}
	if c.AddonCooldownBars <= 0 {
		c.AddonCooldownBars = 3
	}
	if c.RecoveryWatchBars <= 0 {
		c.RecoveryWatchBars = 6
	}
	if c.CompressionThresholdPct <= 0 {
		c.CompressionThresholdPct = 0.012
	}
	if c.MaxDistanceFromLevelPct <= 0 {
		c.MaxDistanceFromLevelPct = 0.004
	}
	if c.StrongCloseMin <= 0 {
		c.StrongCloseMin = 0.70
	}
	if c.StrongCloseMax <= 0 {
		c.StrongCloseMax = 0.30
	}
	if c.ImpulseBodyMinPct <= 0 {
		c.ImpulseBodyMinPct = 0.003
	}
	if c.RetestTolerancePct <= 0 {
		c.RetestTolerancePct = 0.0015
	}
	if c.ReclaimTolerancePct <= 0 {
		c.ReclaimTolerancePct = 0.0007
	}
	if c.StructureLookbackBars <= 0 {
		c.StructureLookbackBars = 5
	}
	if c.WorkingMovePct <= 0 {
		c.WorkingMovePct = 0.003
	}
	if c.RecoveryMovePct <= 0 {
		c.RecoveryMovePct = 0.003
	}
	if c.MinRR <= 0 {
		c.MinRR = 1.5
	}
	if c.SLBufferPct <= 0 {
		c.SLBufferPct = 0.001
	}
	if c.SwingLookbackBars <= 0 {
		c.SwingLookbackBars = 5
	}
	if c.TargetLookbackBars <= 0 {
		c.TargetLookbackBars = 20
	}
	if c.FallbackStopPct <= 0 {
		c.FallbackStopPct = 0.01
	}
	if c.ATRPeriod <= 0 {
		c.ATRPeriod = 14
	}
	if c.ATRStopMult <= 0 {
		c.ATRStopMult = 0.8
	}
}
