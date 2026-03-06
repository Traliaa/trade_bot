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
func uniqTimeframes(tfs ...string) []string {
	m := make(map[string]struct{}, len(tfs))
	out := make([]string, 0, len(tfs))
	for _, tf := range tfs {
		tf = strings.TrimSpace(tf)
		if tf == "" {
			continue
		}
		if _, ok := m[tf]; ok {
			continue
		}
		m[tf] = struct{}{}
		out = append(out, tf)
	}
	return out
}

func toOKXBar(tf string) string {
	switch tf {
	case "1m", "3m", "5m", "15m", "30m":
		return tf
	case "1h":
		return "1H"
	case "2h":
		return "2H"
	case "4h":
		return "4H"
	case "6h":
		return "6H"
	case "12h":
		return "12H"
	case "1d":
		return "1D"
	case "1w":
		return "1W"
	default:
		return tf // fallback, но лучше логнуть
	}
}

func isNonCryptoUnderlying(instID string) bool {
	base := strings.TrimSuffix(instID, "-USDT-SWAP")

	switch base {
	case "AAPL", "AMZN", "MSFT", "META", "INTC", "TSLA", "PLTR", "QQQ", "SPY", "HOOD", "MSTR", "CRCL":
		return true
	case "XAG", "XAU", "XPT", "XPD", "XCU":
		return true
	default:
		return false
	}
}
func isTradeableUSDTSwap(instID string) bool {
	if !strings.HasSuffix(instID, "-USDT-SWAP") {
		return false
	}
	if strings.Contains(instID, "_UM") {
		return false
	}
	if isNonCryptoUnderlying(instID) {
		return false
	}
	return true
}
func coreSymbols() []string {
	return []string{
		"BTC-USDT-SWAP",
		"ETH-USDT-SWAP",
		"SOL-USDT-SWAP",
		"BNB-USDT-SWAP",
		"XRP-USDT-SWAP",
		"DOGE-USDT-SWAP",
		"LINK-USDT-SWAP",
		"AVAX-USDT-SWAP",
		"ADA-USDT-SWAP",
		"DOT-USDT-SWAP",
		"LTC-USDT-SWAP",
		"BCH-USDT-SWAP",
		"SUI-USDT-SWAP",
		"APT-USDT-SWAP",
		"FIL-USDT-SWAP",
		"INJ-USDT-SWAP",
		"AAVE-USDT-SWAP",
		"ICP-USDT-SWAP",
		"ARB-USDT-SWAP",
		"UNI-USDT-SWAP",
	}
}
func mergeUniqueSymbols(groups ...[]string) []string {
	seen := make(map[string]struct{}, 128)

	totalCap := 0
	for _, g := range groups {
		totalCap += len(g)
	}

	out := make([]string, 0, totalCap)

	for _, g := range groups {
		for _, sym := range g {
			if sym == "" {
				continue
			}
			if _, ok := seen[sym]; ok {
				continue
			}
			seen[sym] = struct{}{}
			out = append(out, sym)
		}
	}

	return out
}
func firstN(xs []string, n int) []string {
	if n > len(xs) {
		n = len(xs)
	}
	if n < 0 {
		n = 0
	}
	return xs[:n]
}
