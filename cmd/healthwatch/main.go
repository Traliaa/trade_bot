// Run on a host independent of the bot; no exchange or database credentials.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	"trade_bot/internal/healthwatch"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	target := flag.String("url", "", "explicit /ready URL")
	stateFile := flag.String("state", "", "persistent monitor state JSON path")
	interval := flag.Duration("interval", 30*time.Second, "poll interval, minimum 15s")
	once := flag.Bool("once", false, "perform one check then exit")
	flag.Parse()
	u, err := url.Parse(*target)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Path != "/ready" || u.RawQuery != "" {
		return fmt.Errorf("explicit http(s) /ready URL without credentials/query required")
	}
	if *stateFile == "" || *interval < 15*time.Second {
		return fmt.Errorf("-state file and interval >=15s required")
	}
	webhook := os.Getenv("HEALTHWATCH_WEBHOOK_URL")
	if webhook != "" {
		v, e := url.Parse(webhook)
		if e != nil || v.Scheme != "https" || v.Host == "" || v.User != nil {
			return fmt.Errorf("webhook must be HTTPS without userinfo")
		}
	}
	state := healthwatch.State{}
	data, err := os.ReadFile(*stateFile)
	if err == nil {
		if json.Unmarshal(data, &state) != nil {
			return fmt.Errorf("invalid monitor state; refusing to reset incident history")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot read monitor state")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	tick := time.NewTicker(*interval)
	defer tick.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", *target, nil)
		resp, e := client.Do(req)
		healthy := e == nil && resp.StatusCode == 200
		if resp != nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
		}
		event := state.Observe(time.Now().UTC(), healthy)
		if event != "" {
			body, _ := json.Marshal(map[string]any{"service": "trade-bot", "state": event, "at": time.Now().UTC()})
			delivered := webhook == ""
			if webhook != "" {
				post, _ := http.NewRequestWithContext(ctx, "POST", webhook, bytes.NewReader(body))
				post.Header.Set("Content-Type", "application/json")
				response, err := client.Do(post)
				delivered = err == nil && response.StatusCode >= 200 && response.StatusCode < 300
				if response != nil {
					response.Body.Close()
				}
			}
			if delivered {
				fmt.Println(string(body))
				state.Acknowledge()
			} else {
				fmt.Fprintln(os.Stderr, "health notification delivery failed; will retry")
			}
		}
		if err := saveState(*stateFile, state); err != nil {
			return err
		}
		if *once {
			if !healthy {
				return fmt.Errorf("not ready")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
func saveState(path string, s healthwatch.State) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".healthwatch-*")
	if err != nil {
		return fmt.Errorf("cannot create monitor state")
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("cannot persist monitor state")
	}
	if os.Rename(tmp, path) != nil {
		return fmt.Errorf("cannot replace monitor state")
	}
	return nil
}
