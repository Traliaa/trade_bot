package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	// Watchlist
	DefaultWatchTopN int // .env: DEFAULT_WATCHLIST_TOP_N (по умолчанию 100)

	// Стратегия
	EMAShort      int
	EMALong       int
	RSIPeriod     int
	RSIOverbought float64
	RSIOSold      float64 // из ENV: RSI_OVERSOLD

	// Риск / торговля
	RiskPct          float64
	Leverage         int
	MaxOpenPositions int // .env: MAX_OPEN_POSITIONS (по умолчанию 10)

	// Очередь подтверждений
	ConfirmQueueMax    int           // .env: CONFIRM_QUEUE_MAX (20)
	ConfirmQueuePolicy string        // .env: CONFIRM_QUEUE_POLICY (drop_oldest|drop_same_symbol)
	CooldownPerSymbol  time.Duration // .env: COOLDOWN_PER_SYMBOL (e.g. 60s)
	ConfirmTimeout     time.Duration // .env: CONFIRM_TIMEOUT (e.g. 30s)
	AutoOnTimeout      string        // .env: AUTO_ON_TIMEOUT (off|skip|enter)

	// Mexc API
	MexcAPIKey    string
	MexcAPISecret string

	// Telegram
	TelegramBotToken string
	TelegramChatID   int64
}

func Load() (*Config, error) {
	_ = godotenv.Load()
	cfg := &Config{
		DefaultWatchTopN:   intFromEnv("DEFAULT_WATCHLIST_TOP_N", 100),
		EMAShort:           intFromEnv("EMA_SHORT", 9),
		EMALong:            intFromEnv("EMA_LONG", 21),
		RSIPeriod:          intFromEnv("RSI_PERIOD", 14),
		RSIOverbought:      floatFromEnv("RSI_OVERBOUGHT", 70),
		RSIOSold:           floatFromEnv("RSI_OVERSOLD", 30),
		RiskPct:            floatFromEnv("RISK_PCT", 1.0),
		Leverage:           intFromEnv("LEVERAGE", 5),
		MaxOpenPositions:   intFromEnv("MAX_OPEN_POSITIONS", 10),
		ConfirmQueueMax:    intFromEnv("CONFIRM_QUEUE_MAX", 20),
		ConfirmQueuePolicy: getenvDefault("CONFIRM_QUEUE_POLICY", "drop_same_symbol"),
		CooldownPerSymbol:  durationFromEnv("COOLDOWN_PER_SYMBOL", "60s"),
		ConfirmTimeout:     durationFromEnv("CONFIRM_TIMEOUT", "30s"),
		AutoOnTimeout:      getenvDefault("AUTO_ON_TIMEOUT", "off"),
		MexcAPIKey:         os.Getenv("MEXC_API_KEY"),
		MexcAPISecret:      os.Getenv("MEXC_API_SECRET"),
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
	}
	if v := os.Getenv("TELEGRAM_CHAT_ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.TelegramChatID = id
		}
	}
	if cfg.EMAShort >= cfg.EMALong {
		return nil, fmt.Errorf("EMA_SHORT must be < EMA_LONG")
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
func floatFromEnv(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
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
