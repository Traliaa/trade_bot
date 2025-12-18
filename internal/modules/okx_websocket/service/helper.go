package service

import (
	"fmt"
	"strings"
	"time"
)

func timeframeToDuration(tf string) time.Duration {
	switch tf {
	case "1m":
		return time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1H", "1h":
		return time.Hour
	case "4H", "4h":
		return 4 * time.Hour
	default:
		return 0 // неизвестный — оставим End = Start
	}
}
func okxBar(tf string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(tf)) {
	case "1m", "3m", "5m", "10m", "15m", "30m":
		return tf, nil

	case "60m", "1h":
		return "1H", nil
	case "2h":
		return "2H", nil
	case "4h":
		return "4H", nil
	case "6h":
		return "6H", nil
	case "12h":
		return "12H", nil

	case "1d":
		return "1D", nil
	case "1w":
		return "1W", nil
	case "1mo", "1mth":
		return "1M", nil

	// если вдруг используешь utc бары — тоже приведи к нужному виду:
	case "6hutc":
		return "6Hutc", nil
	case "12hutc":
		return "12Hutc", nil
	case "1dutc":
		return "1Dutc", nil
	case "1wutc":
		return "1Wutc", nil
	case "1mutc":
		return "1Mutc", nil
	case "3mutc":
		return "3Mutc", nil
	}
	return "", fmt.Errorf("unsupported timeframe for OKX bar: %q", tf)
}
