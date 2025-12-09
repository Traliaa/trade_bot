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
	configFilePathENV = "CONFIG_FILE"
	tokenTelegramENV  = "TELEGRAM_TOKEN"
	databaseDSN       = "DATABASE_DSN"
)

// Config ...
type Config struct {
	Telegram struct {
		Token string `yaml:"token"`
	} `yaml:"telegram"`
	DB      string `yaml:"db_dsn"`
	Service struct {
		Host       string `yaml:"host"`
		PublicPort int    `yaml:"public_port"`
		AdminPort  int    `yaml:"admin_port"`
	} `yaml:"service"`

	// Дефолты риска
	// Сколько от депозита мы готовы потерять по СТОПУ, а не по ликвидации
	DefaultRiskPct float64 `yaml:"risk_pct"` // например 1.0 => 1% equity
	// Как считать тейк:
	// вариант 1 — через RR (tp = entry ± RR*dist до SL)
	// вариант 2 — фиксированный процент от цены (tp = entry ± TakeProfitPct%)
	DefaultUseRR         bool    `yaml:"use_rr"`
	DefaultTakeProfitRR  float64 `yaml:"take_profit_rr"`  // например 3.0 => TP = 3R
	DefaultTakeProfitPct float64 `yaml:"take_profit_pct"` // например 1.2 => TP = 1.2%

	DefaultStopPct float64 `yaml:"stop_pct"` // расстояние до SL от цены, напр. 0.5 => 0.5%
	//DefaultTakeProfitRR float64 `yaml:"take_profit_rr"` // мультипликатор TP к стопу, напр. 3.0 => TP = 3 * S

	// Watchlist / раннер
	DefaultWatchTopN   int
	ConfirmQueueMax    int
	ConfirmQueuePolicy string // drop_oldest | drop_same_symbol

	// Дефолты стратегии (создаём юзеру при первом запуске)
	DefaultTimeframe     string
	DefaultEMAShort      int
	DefaultEMALong       int
	DefaultRSIPeriod     int
	DefaultRSIOverbought float64
	DefaultRSIOSold      float64

	DefaultPositionPct      float64
	DefaultLeverage         int
	DefaultMaxOpenPositions int

	// Очередь подтверждений
	DefaultConfirmRequired   bool
	DefaultCooldownPerSymbol time.Duration
	DefaultConfirmTimeout    time.Duration
	DefaultAutoOnTimeout     string

	DefaultDonchianPeriod int // период канала, N свечей (обычно 20)
	DefaultTrendEmaPeriod int // EMA для фильтра тренда (обычно 50)
	DefaultStrategy       string
}

func NewConfig() (*Config, error) {

	configFileName := os.Getenv(configFilePathENV)
	if configFileName == "" {
		configFileName = "values_local.yaml"
	}
	file, err := os.Open("configs/" + configFileName)
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}

	defer func() {
		_ = file.Close()
	}()

	decoder := yaml.NewDecoder(file)
	config := Config{
		DefaultRiskPct:        1.0,
		DefaultStopPct:        0.5,
		DefaultTakeProfitRR:   3.0,
		DefaultDonchianPeriod: 20,
		DefaultTrendEmaPeriod: 50,
		DefaultStrategy:       "donchian",

		DefaultWatchTopN:   intFromEnv("DEFAULT_WATCHLIST_TOP_N", 100),
		ConfirmQueueMax:    intFromEnv("CONFIRM_QUEUE_MAX", 20),
		ConfirmQueuePolicy: getenvDefault("CONFIRM_QUEUE_POLICY", "drop_same_symbol"),

		DefaultTimeframe:     getenvDefault("TIMEFRAME", "1m"),
		DefaultEMAShort:      intFromEnv("EMA_SHORT", 9),
		DefaultEMALong:       intFromEnv("EMA_LONG", 21),
		DefaultRSIPeriod:     intFromEnv("RSI_PERIOD", 14),
		DefaultRSIOverbought: floatFromEnv("RSI_OVERBOUGHT", 70),
		DefaultRSIOSold:      floatFromEnv("RSI_OVERSOLD", 30),

		DefaultPositionPct:      floatFromEnv("POSITION_PCT", 1.0),
		DefaultLeverage:         intFromEnv("LEVERAGE", 20),
		DefaultMaxOpenPositions: intFromEnv("MAX_OPEN_POSITIONS", 10),

		DefaultConfirmRequired:   boolFromEnv("CONFIRM_REQUIRED", true),
		DefaultCooldownPerSymbol: durationFromEnv("COOLDOWN_PER_SYMBOL", "60s"),
		DefaultConfirmTimeout:    durationFromEnv("CONFIRM_TIMEOUT", "30s"),
		DefaultAutoOnTimeout:     getenvDefault("AUTO_ON_TIMEOUT", "off"),
	}
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatalf("Failed to decode config file: %v", err)
	}

	token := os.Getenv(tokenTelegramENV)
	if token != "" {
		config.Telegram.Token = token
	}

	dsn := os.Getenv(databaseDSN)
	if dsn != "" {
		config.DB = dsn
	}

	return &config, nil
}

func getenvRequired(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("env %s is required", key))
	}
	return v
}

func intFromEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func floatFromEnv(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func boolFromEnv(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if v == "1" || v == "true" || v == "TRUE" {
			return true
		}
		if v == "0" || v == "false" || v == "FALSE" {
			return false
		}
	}
	return def
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationFromEnv(key, def string) time.Duration {
	val := getenvDefault(key, def)
	d, err := time.ParseDuration(val)
	if err != nil {
		d, _ = time.ParseDuration(def)
	}
	return d
}
