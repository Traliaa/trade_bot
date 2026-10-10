//go:build linux || darwin

package collect

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const accountConfigPath = "/api/v5/account/config"
const privateFillsPath = "/api/v5/trade/fills-history"

// PrivateClient has no exported generic request method. Its only public read
// workflow verifies account+key permissions each pass before loading fills.
type PrivateClient struct {
	engine      *ArchiveClient
	credentials PrivateCredentials
}

func (PrivateClient) MarshalJSON() ([]byte, error) { return nil, errPrivateEvidence }
func (PrivateClient) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[private archive client]")
}

func NewPrivateClient(store *Store, creds *PrivateCredentials) (*PrivateClient, error) {
	if store == nil || store.requestGate == nil || !creds.valid() {
		return nil, errCredentials
	}
	return &PrivateClient{NewArchiveClient(store), *creds}, nil
}
func (c *PrivateClient) authorize(req *http.Request, at time.Time) {
	ts := at.UTC().Format("2006-01-02T15:04:05.000Z")
	mac := hmac.New(sha256.New, []byte(c.credentials.secret))
	_, _ = mac.Write([]byte(ts + "GET" + req.URL.RequestURI()))
	req.Header.Set("OK-ACCESS-KEY", c.credentials.key)
	req.Header.Set("OK-ACCESS-SIGN", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.credentials.passphrase)
}
func (c *PrivateClient) verifyAccount(ctx context.Context) error {
	body, err := c.engine.get(ctx, "https://www.okx.com"+accountConfigPath, c.authorize)
	if err != nil {
		return err
	}
	if !exactPrivateResponse(body, []string{"perm", "uid"}) {
		return errors.New("research_read_only_account_required")
	}
	var envelope struct {
		Data []struct {
			Permission string `json:"perm"`
			UID        string `json:"uid"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Data) != 1 || envelope.Data[0].Permission != "read_only" || envelope.Data[0].UID != c.credentials.expectedUID {
		return errors.New("research_read_only_account_required")
	}
	return nil
}
func (c *PrivateClient) getFillPage(ctx context.Context, q url.Values) ([]byte, error) {
	if q.Get("instType") != "SWAP" || !usdtSwap.MatchString(q.Get("instId")) || q.Get("limit") != "100" || q.Get("begin") == "" || q.Get("end") == "" {
		return nil, errors.New("research_private_query_invalid")
	}
	for key, values := range q {
		if len(values) != 1 {
			return nil, errors.New("research_private_query_invalid")
		}
		switch key {
		case "instType", "instId", "limit":
		case "begin", "end":
			v, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil || v < 0 || strconv.FormatInt(v, 10) != values[0] {
				return nil, errors.New("research_private_query_invalid")
			}
		case "after":
			if !privateID.MatchString(values[0]) {
				return nil, errors.New("research_private_query_invalid")
			}
		default:
			return nil, errors.New("research_private_query_invalid")
		}
	}
	target := (&url.URL{Scheme: "https", Host: "www.okx.com", Path: privateFillsPath, RawQuery: q.Encode()}).String()
	return c.engine.get(ctx, target, c.authorize)
}
