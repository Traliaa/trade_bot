package sessions

import (
	"fmt"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
)

func calcBEPrice(st *models.PositionTrailState, cfg models.Settings) float64 {
	if st == nil || st.Entry <= 0 || st.RiskDist <= 0 {
		return 0
	}

	offsetR := cfg.TrailingConfig.BEOffsetR
	if offsetR < 0 {
		offsetR = 0
	}

	if st.PosSide == "long" {
		return st.Entry + offsetR*st.RiskDist
	}
	return st.Entry - offsetR*st.RiskDist
}

func approxAtOrBeyondBE(st *models.PositionTrailState, cfg models.Settings, sl float64) bool {
	if st == nil || sl <= 0 {
		return false
	}

	be := calcBEPrice(st, cfg)
	if be <= 0 {
		return false
	}

	const eps = 1e-12

	if st.PosSide == "long" {
		return sl >= be-eps
	}
	return sl <= be+eps
}

func approxLocksProfit(st *models.PositionTrailState, prevSL, newSL float64) bool {
	if st == nil || newSL <= 0 || st.Entry <= 0 {
		return false
	}

	switch st.PosSide {
	case "long":
		return newSL > st.Entry && newSL > prevSL
	case "short":
		return newSL < st.Entry && newSL < prevSL
	default:
		return false
	}
}

func shouldImproveSL(st *models.PositionTrailState, candidate float64) bool {
	if st == nil || candidate <= 0 || st.SL <= 0 {
		return false
	}

	if st.PosSide == "long" {
		return candidate > st.SL
	}

	return candidate < st.SL
}
func openPositionUnrealizedPnLPct(pos models.OpenPosition, payload models.TradePayload, currentPrice float64) float64 {
	if pos.UnrealizedPct != 0 {
		return pos.UnrealizedPct
	}
	return calcPriceMovePct(payload, currentPrice)
}
func decideTrail15m(
	st *models.PositionTrailState,
	cfg models.Settings,
	lastPrice float64,
	slotEnd time.Time,
) models.TrailDecision {
	R := st.RiskDist
	if R <= 0 || st.Entry <= 0 || st.SL <= 0 {
		return models.TrailDecision{}
	}

	slot := helper.TrailSlot15m(slotEnd)
	if !st.LastTrailEnd.IsZero() && st.LastTrailEnd.Equal(slot) {
		return models.TrailDecision{}
	}

	improves := func(candidate float64) bool {
		if st.PosSide == "long" {
			return candidate > st.SL
		}
		return candidate < st.SL
	}

	minImprove := 0.10 * R
	improvesEnough := func(candidate float64) bool {
		if st.PosSide == "long" {
			return candidate-st.SL >= minImprove
		}
		return st.SL-candidate >= minImprove
	}

	var mfeR float64
	if st.PosSide == "long" {
		mfeR = (st.MFE - st.Entry) / R
	} else {
		mfeR = (st.Entry - st.MFE) / R
	}

	var currentR float64
	if lastPrice > 0 {
		if st.PosSide == "long" {
			currentR = (lastPrice - st.Entry) / R
		} else {
			currentR = (st.Entry - lastPrice) / R
		}
	}

	// --- EARLY FAIL ---
	if cfg.TrailingConfig.EarlyTimeStopBars > 0 &&
		cfg.TrailingConfig.EarlyTimeStopMinMFER > 0 &&
		!st.OpenedAt.IsZero() {

		earlyDur := time.Duration(cfg.TrailingConfig.EarlyTimeStopBars) * 15 * time.Minute
		if slotEnd.Sub(st.OpenedAt) >= earlyDur && mfeR < cfg.TrailingConfig.EarlyTimeStopMinMFER {
			return models.TrailDecision{
				Close:  true,
				Reason: models.CloseReasonTimeStopEarly,
				Note:   "TIME_STOP_EARLY",
			}
		}
	}

	// --- STALE MODE ---
	sc := models.GetStaleConfig(cfg)

	if !st.OpenedAt.IsZero() {
		staleAfter := time.Duration(sc.AfterBars) * 15 * time.Minute

		// 1) Входим в stale mode, но не закрываем сразу
		if !st.IsStale &&
			slotEnd.Sub(st.OpenedAt) >= staleAfter &&
			mfeR < sc.MinMFER {

			st.IsStale = true
			st.StaleSince = slotEnd
			st.StaleMarkedAtR = currentR
		}

		// 2) Если позиция stale — управляем умнее
		if st.IsStale {
			graceDur := time.Duration(sc.GraceBars) * 15 * time.Minute
			inGrace := !st.StaleSince.IsZero() && slotEnd.Sub(st.StaleSince) < graceDur

			// stale, но вышла в маленький плюс -> закрываем
			if currentR >= sc.ExitProfitR {
				return models.TrailDecision{
					Close:  true,
					Reason: models.CloseReasonTimeStopStale,
					Note:   "STALE_EXIT_PROFIT",
				}
			}

			// stale и около нуля -> подтягиваем стоп ближе к BE
			if currentR >= sc.NearBER {
				cand := calcStaleBEPrice(st, cfg)
				if cand > 0 && improves(cand) {
					return models.TrailDecision{
						NewSL:  cand,
						MoveSL: true,
						Reason: models.CloseReasonBreakEven,
						Note:   "STALE_TO_BE",
					}
				}
			}

			// Во время grace period в минус не режем сразу
			if inGrace {
				// но если уже сдвинули в BE/LOCK, можно дать дальше работать обычной логике
			} else {
				// stale и стало явно хуже
				if currentR <= sc.MaxAdverseR {
					return models.TrailDecision{
						Close:  true,
						Reason: models.CloseReasonTimeStopStale,
						Note:   "STALE_EXIT_MAX_ADVERSE",
					}
				}

				// stale и ухудшилось относительно момента входа в stale
				if currentR <= st.StaleMarkedAtR-sc.WorseByR {
					return models.TrailDecision{
						Close:  true,
						Reason: models.CloseReasonTimeStopStale,
						Note:   "STALE_EXIT_DEGRADE",
					}
				}
			}
		}
	}

	// --- PARTIAL ---
	if cfg.TrailingConfig.PartialEnabled &&
		!st.TookPartial &&
		mfeR >= cfg.TrailingConfig.PartialTriggerR &&
		st.Size > 0 {

		closeSz := st.Size * cfg.TrailingConfig.PartialCloseFrac
		if closeSz > 0 {
			newSL := calcBEPrice(st, cfg)

			return models.TrailDecision{
				CloseSize: closeSz,
				Reason:    models.CloseReasonPartialExit,
				Note: fmt.Sprintf(
					"PARTIAL@%.2fR (%.0f%%) + SL->BE",
					cfg.TrailingConfig.PartialTriggerR,
					cfg.TrailingConfig.PartialCloseFrac*100,
				),
				MoveSLAfterPartial: true,
				NewSLAfterPartial:  newSL,
			}
		}
	}

	// --- BE ---
	if !st.MovedToBE && mfeR >= cfg.TrailingConfig.BETriggerR {
		cand := calcBEPrice(st, cfg)
		if cand > 0 && improves(cand) {
			return models.TrailDecision{
				NewSL:  cand,
				MoveSL: true,
				Reason: models.CloseReasonBreakEven,
				Note:   "BE",
			}
		}
	}

	// --- LOCK ---
	if !st.LockedProfit && mfeR >= cfg.TrailingConfig.LockTriggerR {
		var cand float64
		if st.PosSide == "long" {
			cand = st.Entry + cfg.TrailingConfig.LockOffsetR*R
		} else {
			cand = st.Entry - cfg.TrailingConfig.LockOffsetR*R
		}

		if improves(cand) && improvesEnough(cand) {
			return models.TrailDecision{
				NewSL:  cand,
				MoveSL: true,
				Reason: models.CloseReasonLockProfit,
				Note:   "LOCK",
			}
		}
	}

	return models.TrailDecision{}
}
func calcStaleBEPrice(st *models.PositionTrailState, cfg models.Settings) float64 {
	if st == nil || st.Entry <= 0 || st.RiskDist <= 0 {
		return 0
	}

	sc := models.GetStaleConfig(cfg)
	offsetR := sc.TightenToBER

	if st.PosSide == "long" {
		return st.Entry + offsetR*st.RiskDist
	}
	return st.Entry - offsetR*st.RiskDist
}
