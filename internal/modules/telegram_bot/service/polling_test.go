package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type pollingStub struct {
	webhook func() (tgbot.WebhookInfo, error)
	updates func(tgbot.UpdateConfig) ([]tgbot.Update, error)
}

func (s pollingStub) GetWebhookInfo() (tgbot.WebhookInfo, error)              { return s.webhook() }
func (s pollingStub) GetUpdates(c tgbot.UpdateConfig) ([]tgbot.Update, error) { return s.updates(c) }

func TestPollingWebhookWaitsWithoutConsumingUpdates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checks, polls, waits := 0, 0, 0
	var events []string
	err := runUpdatePolling(ctx, pollingStub{
		webhook: func() (tgbot.WebhookInfo, error) {
			checks++
			if checks <= 2 {
				return tgbot.WebhookInfo{URL: "https://private.invalid/secret"}, nil
			}
			return tgbot.WebhookInfo{}, nil
		},
		updates: func(tgbot.UpdateConfig) ([]tgbot.Update, error) { polls++; cancel(); return nil, ctx.Err() },
	}, func(tgbot.Update) { t.Fatal("unexpected update") }, func(ctx context.Context, d time.Duration) error {
		waits++
		if d < 30*time.Second {
			t.Fatalf("webhook retry too fast: %s", d)
		}
		return nil
	}, func(code string) { events = append(events, code) })
	if !errors.Is(err, context.Canceled) || checks != 3 || polls != 1 || waits != 2 {
		t.Fatalf("err=%v checks=%d polls=%d waits=%d", err, checks, polls, waits)
	}
	if len(events) != 1 || events[0] != "webhook_active" {
		t.Fatalf("repeated or unsafe diagnostics: %v", events)
	}
}

func TestPollingRetriesAndRecoversWithOffset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	polls, checks := 0, 0
	var delays []time.Duration
	var events []string
	var received []int
	err := runUpdatePolling(ctx, pollingStub{
		webhook: func() (tgbot.WebhookInfo, error) { checks++; return tgbot.WebhookInfo{}, nil },
		updates: func(c tgbot.UpdateConfig) ([]tgbot.Update, error) {
			polls++
			if polls <= 3 {
				return nil, &tgbot.Error{Code: 409, Message: "private-token-must-not-be-logged"}
			}
			if polls == 4 {
				return []tgbot.Update{{UpdateID: 7}, {UpdateID: 8}}, nil
			}
			if c.Offset != 9 {
				t.Fatalf("offset=%d, want 9", c.Offset)
			}
			cancel()
			return nil, ctx.Err()
		},
	}, func(u tgbot.Update) { received = append(received, u.UpdateID) }, func(ctx context.Context, d time.Duration) error { delays = append(delays, d); return nil }, func(code string) { events = append(events, code) })
	if !errors.Is(err, context.Canceled) || checks < 2 || len(received) != 2 {
		t.Fatalf("err=%v checks=%d received=%v", err, checks, received)
	}
	if len(delays) != 3 || delays[1] <= delays[0] || delays[2] <= delays[1] {
		t.Fatalf("backoff=%v", delays)
	}
	if len(events) != 2 || events[0] != "polling_conflict" || events[1] != "polling_recovered" {
		t.Fatalf("events=%v", events)
	}
}

func TestPollingStopsOnInvalidCredentials(t *testing.T) {
	for _, code := range []int{401, 403} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			err := runUpdatePolling(context.Background(), pollingStub{
				webhook: func() (tgbot.WebhookInfo, error) {
					return tgbot.WebhookInfo{}, &tgbot.Error{Code: code, Message: "secret"}
				},
				updates: func(tgbot.UpdateConfig) ([]tgbot.Update, error) {
					t.Fatal("polled after auth failure")
					return nil, nil
				},
			}, func(tgbot.Update) {}, func(context.Context, time.Duration) error { t.Fatal("retried permanent auth failure"); return nil }, func(string) {})
			if err == nil || err.Error() != "telegram authorization failed" {
				t.Fatalf("unsafe or missing error: %v", err)
			}
		})
	}
}

type pollingTransport func(*http.Request) (*http.Response, error)

func (f pollingTransport) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestPollingStopCancelsTransportAndAllowsRestart(t *testing.T) {
	started := make(chan struct{}, 2)
	client := pollingTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/getWebhookInfo") {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"url":"","pending_update_count":0}}`))}, nil
		}
		if !strings.HasSuffix(req.URL.Path, "/getUpdates") {
			t.Errorf("unexpected request: %s", req.URL.Path)
		}
		started <- struct{}{}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	bot := &tgbot.BotAPI{Token: "synthetic", Client: client}
	bot.SetAPIEndpoint("https://fixture.invalid/bot%s/%s")
	receiver := &Telegram{bot: bot}
	receiver.Stop() // safe even before startup
	for i := 0; i < 2; i++ {
		done := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		go func() { done <- receiver.Start(ctx) }()
		select {
		case <-started:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("poll did not start")
		}
		if err := receiver.Start(ctx); err == nil {
			cancel()
			t.Fatal("second receiver started")
		}
		receiver.Stop()
		receiver.Stop()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("unexpected shutdown: %v", err)
			}
		case <-time.After(time.Second):
			cancel()
			t.Fatal("in-flight request was not canceled")
		}
		cancel()
	}
}

func TestPollingRetryWaitIsCancelable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForPollingRetry(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPollingStopCancelsCallbackAcknowledgement(t *testing.T) {
	inHandler := make(chan struct{})
	transport := pollingTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch {
		case strings.HasSuffix(req.URL.Path, "/getWebhookInfo"):
			body = `{"ok":true,"result":{"url":""}}`
		case strings.HasSuffix(req.URL.Path, "/getUpdates"):
			body = `{"ok":true,"result":[{"update_id":1,"callback_query":{"id":"synthetic","data":"unknown","from":{"id":1},"message":{"message_id":1,"chat":{"id":1}}}}]}`
		case strings.HasSuffix(req.URL.Path, "/answerCallbackQuery"):
			close(inHandler)
			<-req.Context().Done()
			return nil, req.Context().Err()
		default:
			t.Errorf("unexpected method: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	bot := &tgbot.BotAPI{Token: "synthetic", Client: &receiverHTTPClient{client: transport}}
	bot.SetAPIEndpoint("https://fixture.invalid/bot%s/%s")
	receiver := &Telegram{bot: bot}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- receiver.Start(ctx) }()
	select {
	case <-inHandler:
	case <-time.After(time.Second):
		t.Fatal("callback was not handled")
	}
	receiver.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback transport ignored shutdown")
	}
}

func TestPollingReportsNewIncidentCausesOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checks := 0
	var events []string
	_ = runUpdatePolling(ctx, pollingStub{
		webhook: func() (tgbot.WebhookInfo, error) {
			checks++
			if checks == 5 {
				cancel()
				return tgbot.WebhookInfo{}, ctx.Err()
			}
			if checks%2 == 1 {
				return tgbot.WebhookInfo{}, errors.New("network")
			}
			return tgbot.WebhookInfo{URL: "https://fixture.invalid"}, nil
		},
		updates: func(tgbot.UpdateConfig) ([]tgbot.Update, error) { t.Fatal("unexpected poll"); return nil, nil },
	}, func(tgbot.Update) {}, func(context.Context, time.Duration) error { return nil }, func(code string) { events = append(events, code) })
	if len(events) != 2 || events[0] != "polling_unavailable" || events[1] != "webhook_active" {
		t.Fatalf("lost cause or repeated logs: %v", events)
	}
}

func TestPollingUnavailableIsBoundedAndDoesNotSpam(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits, events := 0, 0
	err := runUpdatePolling(ctx, pollingStub{
		webhook: func() (tgbot.WebhookInfo, error) {
			return tgbot.WebhookInfo{}, errors.New("https://secret.invalid/token")
		},
		updates: func(tgbot.UpdateConfig) ([]tgbot.Update, error) {
			t.Fatal("must not poll after failed preflight")
			return nil, nil
		},
	}, func(tgbot.Update) {}, func(ctx context.Context, d time.Duration) error {
		if d < 3*time.Second || d > time.Minute {
			t.Fatalf("unbounded backoff: %s", d)
		}
		waits++
		if waits == 12 {
			cancel()
			return ctx.Err()
		}
		return nil
	}, func(code string) {
		events++
		if code != "polling_unavailable" {
			t.Fatalf("unsafe event: %s", code)
		}
	})
	if !errors.Is(err, context.Canceled) || waits != 12 || events != 1 {
		t.Fatalf("err=%v waits=%d events=%d", err, waits, events)
	}
}

func TestPollingRateLimitAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runUpdatePolling(ctx, pollingStub{
		webhook: func() (tgbot.WebhookInfo, error) { return tgbot.WebhookInfo{}, nil },
		updates: func(tgbot.UpdateConfig) ([]tgbot.Update, error) {
			return nil, &tgbot.Error{Code: 429, ResponseParameters: tgbot.ResponseParameters{RetryAfter: 120}}
		},
	}, func(tgbot.Update) {}, func(ctx context.Context, d time.Duration) error {
		if d < 120*time.Second {
			t.Fatalf("ignored retry_after: %s", d)
		}
		cancel()
		return ctx.Err()
	}, func(string) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
