package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestReceiverHTTPContextLivesUntilBodyClose(t *testing.T) {
	var sent context.Context
	client := &receiverHTTPClient{client: pollingTransport(func(req *http.Request) (*http.Response, error) {
		sent = req.Context()
		return &http.Response{Body: io.NopCloser(strings.NewReader("body"))}, nil
	})}
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.bind(lifetime)
	req, _ := http.NewRequest("GET", "https://fixture.invalid", nil)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Err() != nil {
		t.Fatal("headers prematurely canceled body download")
	}
	client.bind(context.Background())
	cancel()
	select {
	case <-sent.Done():
	case <-time.After(time.Second):
		t.Fatal("rebinding detached an old request")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReceiverHTTPPreservesCallerCancellation(t *testing.T) {
	client := &receiverHTTPClient{client: pollingTransport(func(req *http.Request) (*http.Response, error) {
		if !errors.Is(req.Context().Err(), context.Canceled) {
			t.Error("caller context was replaced")
		}
		return nil, req.Context().Err()
	})}
	client.bind(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://fixture.invalid", nil)
	_, err := client.Do(req)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestReceiverHTTPTransportErrorDoesNotExposeTokenURL(t *testing.T) {
	client := &receiverHTTPClient{client: pollingTransport(func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Post", URL: "https://fixture.invalid/botSECRET/sendMessage", Err: errors.New("network")}
	})}
	client.bind(context.Background())
	req, _ := http.NewRequest("POST", "https://fixture.invalid", nil)
	_, err := client.Do(req)
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "https://") {
		t.Fatal("transport error was not sanitized")
	}
}
