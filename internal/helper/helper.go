package helper

import (
	"math"
	"strings"
	"time"
)

func NormTF(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	s = strings.TrimPrefix(s, "candle")
	switch s {
	case "60m", "1h":
		return "1h"
	case "15m":
		return "15m"
	case "5m":
		return "5m"
	case "10m":
		return "10m"
	default:
		return s
	}
}

func TrailKey(instId, posSide string) string { return instId + ":" + posSide }

func TrailSlot15m(t time.Time) time.Time {
	// слот по Unix: каждые 900 секунд
	sec := t.Unix()
	sec -= sec % 900
	return time.Unix(sec, 0).In(t.Location())
}

func RoundDownToTick(px, tick float64) float64 {
	if tick <= 0 {
		return px
	}
	steps := math.Floor(px/tick + 1e-12)
	return steps * tick
}

func RoundUpToTick(px, tick float64) float64 {
	if tick <= 0 {
		return px
	}
	steps := math.Ceil(px/tick - 1e-12)
	return steps * tick
}

func SplitTrailKey(key string) (instID string, posSide string, ok bool) {
	// ожидаем формат "instId:posSide"
	i := strings.LastIndexByte(key, ':')
	if i <= 0 || i >= len(key)-1 {
		return "", "", false
	}

	instID = key[:i]
	posSide = key[i+1:]

	if instID == "" {
		return "", "", false
	}

	switch posSide {
	case "long", "short":
		// ok
	default:
		return "", "", false
	}

	return instID, posSide, true
}
