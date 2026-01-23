package config

import (
	"log"
	"os"
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
}

type UserDefaultsConfig struct {
	// стартовые дефолты для нового юзера
	DefaultLeverage         int     `yaml:"default_leverage"`
	DefaultMaxOpenPositions int     `yaml:"default_max_open_positions"`
	DefaultPositionPct      float64 `yaml:"default_position_pct"`
	DefaultRiskPct          float64 `yaml:"default_risk_pct"`
	DefaultStopPct          float64 `yaml:"default_stop_pct"`
	DefaultTakeProfitRR     float64 `yaml:"default_take_profit_rr"`

	DefaultConfirmRequired   bool          `yaml:"default_confirm_required"`
	DefaultConfirmTimeout    time.Duration `yaml:"default_confirm_timeout"`
	DefaultCooldownPerSymbol time.Duration `yaml:"default_cooldown_per_symbol"`
}

type TrailingDefaultsConfig struct {
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

func NewConfig() (*Config, error) {
	// дефолты на случай пустого yaml
	cfg := &Config{
		DefaultTrailing: TrailingDefaultsConfig{
			BETriggerR:       0.6,
			BEOffsetR:        0.0,
			LockTriggerR:     0.9,
			LockOffsetR:      0.3,
			TimeStopBars:     12,
			TimeStopMinMFER:  0.3,
			PartialEnabled:   true,
			PartialTriggerR:  0.9,
			PartialCloseFrac: 0.5,
		},
	}

	// Service defaults
	cfg.Service.Host = "localhost"
	cfg.Service.PublicPort = 3000
	cfg.Service.AdminPort = 3001
	cfg.Service.Workers = 5

	// Strategy defaults
	cfg.Strategy.LTF = "15m"
	cfg.Strategy.HTF = "1h"
	cfg.Strategy.DonchianPeriod = 20
	cfg.Strategy.MinChannelPct = 0.012
	cfg.Strategy.MinBodyPct = 0.004
	cfg.Strategy.BreakoutPct = 0.0025
	cfg.Strategy.HTFEmaFast = 50
	cfg.Strategy.HTFEmaSlow = 200
	cfg.Strategy.MinWarmupLTF = 20
	cfg.Strategy.MinWarmupHTF = 200
	cfg.Strategy.ExpectedSymbols = 100
	cfg.Strategy.ProgressEvery = 2 * time.Minute
	cfg.Strategy.WatchTopN = 100

	// User defaults (только стартовые)
	cfg.UserDefaults.DefaultLeverage = 15
	cfg.UserDefaults.DefaultMaxOpenPositions = 6
	cfg.UserDefaults.DefaultPositionPct = 1.0
	cfg.UserDefaults.DefaultRiskPct = 0.5
	cfg.UserDefaults.DefaultStopPct = 3.0
	cfg.UserDefaults.DefaultTakeProfitRR = 2.0
	cfg.UserDefaults.DefaultConfirmRequired = true
	cfg.UserDefaults.DefaultConfirmTimeout = 30 * time.Second
	cfg.UserDefaults.DefaultCooldownPerSymbol = 6 * time.Hour

	// --- читаем yaml ---
	configFileName := os.Getenv(configFilePathENV)
	if configFileName == "" {
		configFileName = "values_local.yaml"
	}
	file, err := os.Open("configs/" + configFileName)
	if err != nil {
		log.Printf("Failed to open config file: %v", err)
		return nil, err
	}
	defer file.Close()

	dec := yaml.NewDecoder(file)
	if err := dec.Decode(cfg); err != nil {
		log.Printf("Failed to decode config file: %v", err)
		return nil, err
	}

	// --- env overrides ---
	if v := os.Getenv(tokenTelegramENV); v != "" {
		cfg.Telegram.Token = v
	}
	if v := os.Getenv(databaseDSN); v != "" {
		cfg.DB = v
	}
	if v := os.Getenv(ServiceTelegramChatID); v != "" {
		cfg.ServiceTelegramChatID = atoiDefault(v, cfg.ServiceTelegramChatID)
	}

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

	// sanity: если забыли токен
	if cfg.Telegram.Token == "" {
		log.Printf("WARN: telegram.token is empty (env %s or yaml telegram.token)", tokenTelegramENV)
	}

	return cfg, nil
}

// helpers (минимально)
func atoiDefault(s string, def int) int {
	n := def
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return def
		}
	}
	// простейший парсер без strconv (можно заменить на strconv.Atoi)
	n = 0
	for i := 0; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}
