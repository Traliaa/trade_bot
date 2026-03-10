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
	LTF string `yaml:"ltf"` // напр "15m"
	HTF string `yaml:"htf"` // напр "1h"

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
}

type UserDefaultsConfig struct {
	// стартовые дефолты для нового юзера
	DefaultLeverage         int     `yaml:"default_leverage"`
	DefaultMaxOpenPositions int     `yaml:"default_max_open_positions"`
	DefaultPositionPct      float64 `yaml:"default_position_pct"`
	DefaultRiskPct          float64 `yaml:"default_risk_pct"`
	DefaultStopPct          float64 `yaml:"default_stop_pct"`
	DefaultTakeProfitRR     float64 `yaml:"default_take_profit_rr"`
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
