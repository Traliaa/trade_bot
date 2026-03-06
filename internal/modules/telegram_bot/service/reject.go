package service

import (
	"context"
	"fmt"
	"strings"
	"trade_bot/internal/models"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const adminChat = 213532199

func (t *Telegram) sendRejects(ctx context.Context, chatID int64, reset bool) {
	if chatID != adminChat {
		_, _ = t.Send(ctx, chatID, "⛔️ Доступно только в админ-чате.")
		return
	}

	snap := t.router.StrategyRejects(reset)

	var b strings.Builder
	b.WriteString("📊 <b>Reject статистика стратегии</b>\n\n")

	b.WriteString(fmt.Sprintf(
		"Период: %s — %s\n",
		snap.From.Format("15:04:05"),
		snap.To.Format("15:04:05"),
	))
	b.WriteString(fmt.Sprintf("Всего отклонений: <b>%d</b>\n\n", snap.Total))

	if snap.Total == 0 || len(snap.Top) == 0 {
		b.WriteString("Пока нет данных.\n")
	} else {
		b.WriteString("<b>Топ причин:</b>\n")
		for i, it := range snap.Top {
			b.WriteString(fmt.Sprintf(
				"%d) <code>%s</code> — %d\n",
				i+1,
				rejectReasonLabel(it.Reason),
				it.Count,
			))
		}
	}

	// pending-ветка отдельно
	if snap.Counts != nil {
		pendingTotal :=
			snap.Counts[models.RejectPendingCooldown] +
				snap.Counts[models.RejectPendingAdverseMove] +
				snap.Counts[models.RejectPendingExpiredNoTouch] +
				snap.Counts[models.RejectPendingExpiredNoConfirm] +
				snap.Counts[models.RejectPendingBadConfirmCandle] +
				snap.Counts[models.RejectPendingShallowRetest]

		if pendingTotal > 0 {
			b.WriteString("\n<b>Pending:</b>\n")
			if v := snap.Counts[models.RejectPendingCooldown]; v > 0 {
				b.WriteString(fmt.Sprintf("• cooldown — <code>%d</code>\n", v))
			}
			if v := snap.Counts[models.RejectPendingAdverseMove]; v > 0 {
				b.WriteString(fmt.Sprintf("• adverse_move — <code>%d</code>\n", v))
			}
			if v := snap.Counts[models.RejectPendingExpiredNoTouch]; v > 0 {
				b.WriteString(fmt.Sprintf("• expired_no_touch — <code>%d</code>\n", v))
			}
			if v := snap.Counts[models.RejectPendingExpiredNoConfirm]; v > 0 {
				b.WriteString(fmt.Sprintf("• expired_no_confirm — <code>%d</code>\n", v))
			}
			if v := snap.Counts[models.RejectPendingShallowRetest]; v > 0 {
				b.WriteString(fmt.Sprintf("• shallow_retest — <code>%d</code>\n", v))
			}
			if v := snap.Counts[models.RejectPendingBadConfirmCandle]; v > 0 {
				b.WriteString(fmt.Sprintf("• bad_confirm_candle — <code>%d</code>\n", v))
			}
		}

		if v := snap.Counts[models.RejectWeakClose]; v > 0 {
			b.WriteString(fmt.Sprintf("\nWeak close total: <code>%d</code>\n", v))
		}
	}

	if reset {
		b.WriteString("\n✅ Счётчики сброшены.\n")
	}

	tu, lastSig, lastTune := t.router.StrategyTuning()

	b.WriteString("\n<b>Текущие пороги:</b>\n")
	b.WriteString(fmt.Sprintf("• CloseUpMin: <code>%.2f</code>\n", tu.CloseUpMin))
	b.WriteString(fmt.Sprintf("• CloseDnMax: <code>%.2f</code>\n", tu.CloseDnMax))
	b.WriteString(fmt.Sprintf("• MinBodyPct: <code>%.4f</code>\n", tu.MinBodyPct))
	b.WriteString(fmt.Sprintf("• MinChannelPct: <code>%.4f</code>\n", tu.MinChannelPct))
	b.WriteString(fmt.Sprintf("• BreakoutPct: <code>%.4f</code>\n", tu.BreakoutPct))

	if snap.AvgCloseUp > 0 {
		b.WriteString(fmt.Sprintf(
			"Avg weak_close_up closePos: <code>%.3f</code> (порог %.2f)\n",
			snap.AvgCloseUp, tu.CloseUpMin,
		))
	}
	if snap.AvgCloseDown > 0 {
		b.WriteString(fmt.Sprintf(
			"Avg weak_close_down closePos: <code>%.3f</code> (порог %.2f)\n",
			snap.AvgCloseDown, tu.CloseDnMax,
		))
	}

	if !lastSig.IsZero() {
		b.WriteString(fmt.Sprintf("Последний сигнал: <code>%s</code>\n", lastSig.Format("15:04:05")))
	} else {
		b.WriteString("Последний сигнал: <code>нет</code>\n")
	}

	if !lastTune.IsZero() {
		b.WriteString(fmt.Sprintf("Последний авто-тюн: <code>%s</code>\n", lastTune.Format("15:04:05")))
	} else {
		b.WriteString("Последний авто-тюн: <code>нет</code>\n")
	}

	msg := tgbot.NewMessage(chatID, b.String())
	msg.ParseMode = "HTML"
	_, _ = t.SendMessage(ctx, msg)
}
func rejectReasonLabel(r models.RejectReason) string {
	switch r {
	case models.RejectWeakClose:
		return "weak_close (up+down)"
	case models.RejectWeakCloseUp:
		return "weak_close_up"
	case models.RejectWeakCloseDown:
		return "weak_close_down"
	case models.RejectPendingCooldown:
		return "pending_cooldown"
	case models.RejectPendingAdverseMove:
		return "pending_adverse_move"
	case models.RejectPendingExpiredNoTouch:
		return "pending_expired_no_touch"
	case models.RejectPendingExpiredNoConfirm:
		return "pending_expired_no_confirm"
	case models.RejectPendingShallowRetest:
		return "pending_shallow_retest"
	case models.RejectPendingBadConfirmCandle:
		return "pending_bad_confirm_candle"
	case models.RejectBreakoutTooLong:
		return "breakout_too_long"
	default:
		return string(r)
	}
}
