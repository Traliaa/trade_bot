package service

import "time"

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
