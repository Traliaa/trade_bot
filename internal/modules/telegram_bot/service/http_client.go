package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// The shared client cancels both polling and handler replies on shutdown.
// Requests keep their caller context; changing lifetime for a subsequent Start
// cannot revive requests belonging to the previous run.
type receiverHTTPClient struct {
	client   tgbot.HTTPClient
	mu       sync.RWMutex
	lifetime context.Context
}

func (c *receiverHTTPClient) bind(ctx context.Context) {
	c.mu.Lock()
	c.lifetime = ctx
	c.mu.Unlock()
}

func (c *receiverHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.RLock()
	lifetime := c.lifetime
	c.mu.RUnlock()
	if lifetime == nil {
		return c.client.Do(req)
	} // initial getMe, before Start
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(lifetime, cancel)
	release := func() { stop(); cancel() }
	if lifetime.Err() != nil {
		release()
		return nil, lifetime.Err()
	}
	response, err := c.client.Do(req.WithContext(ctx))
	if err != nil {
		contextErr := ctx.Err()
		release()
		if contextErr != nil {
			return response, contextErr
		}
		// net/url errors include the Bot API token in their URL. Callers may log
		// errors, so never return the raw transport error across this boundary.
		return response, errors.New("telegram transport request failed")
	}
	// Cancellation must remain active while the library reads the response body.
	response.Body = &cancelOnCloseBody{ReadCloser: response.Body, release: release}
	return response, nil
}

type cancelOnCloseBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *cancelOnCloseBody) Close() error {
	b.once.Do(b.release)
	return b.ReadCloser.Close()
}
