package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type updateSource interface {
	GetWebhookInfo() (tgbot.WebhookInfo, error)
	GetUpdates(tgbot.UpdateConfig) ([]tgbot.Update, error)
}

var errWebhookActive = errors.New("webhook active")

// runUpdatePolling owns the offset and retries. Never deletes a webhook or
// drops pending updates. Diagnostics are fixed codes, never Telegram/HTTP text.
func runUpdatePolling(ctx context.Context, source updateSource, handle func(tgbot.Update), wait func(context.Context, time.Duration) error, report func(string)) error {
	config := tgbot.NewUpdate(0)
	config.Timeout = 30
	checkWebhook := true
	backoff := 3 * time.Second
	reported := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		if checkWebhook {
			var info tgbot.WebhookInfo
			info, err = source.GetWebhookInfo()
			if err == nil && info.URL != "" {
				err = errWebhookActive
			}
			if err == nil {
				checkWebhook = false
			}
		}
		if err == nil {
			var updates []tgbot.Update
			updates, err = source.GetUpdates(config)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil {
				if len(reported) != 0 {
					report("polling_recovered")
					clear(reported)
				}
				backoff = 3 * time.Second
				for _, update := range updates {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if update.UpdateID < config.Offset {
						continue
					}
					handle(update)
					config.Offset = update.UpdateID + 1
				}
				continue
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		code := "polling_unavailable"
		delay := backoff
		var apiError *tgbot.Error
		switch {
		case errors.Is(err, errWebhookActive):
			code = "webhook_active"
			if delay < 30*time.Second {
				delay = 30 * time.Second
			}
		case errors.As(err, &apiError):
			switch apiError.Code {
			case 401, 403:
				return errors.New("telegram authorization failed")
			case 409:
				code = "polling_conflict"
			case 429:
				code = "polling_rate_limited"
				// Bound conversion before multiplying to avoid duration overflow.
				seconds := apiError.RetryAfter
				if seconds > 86400 {
					seconds = 86400
				}
				if retry := time.Duration(seconds) * time.Second; retry > delay {
					delay = retry
				}
			}
		}
		if !reported[code] {
			report(code)
			reported[code] = true
		}
		checkWebhook = true
		if err := wait(ctx, delay); err != nil {
			return err
		}
		backoff *= 2
		if backoff > time.Minute {
			backoff = time.Minute
		}
	}
}

func waitForPollingRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// A per-run client copy attaches cancellation without changing the shared
// notification client or the library's global logger.
type pollingHTTPClient struct {
	ctx    context.Context
	client tgbot.HTTPClient
}

func (c pollingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req.WithContext(c.ctx))
}
