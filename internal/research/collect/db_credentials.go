//go:build linux || darwin

package collect

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBCredentials never discovers a DSN or password from the trading service.
type DBCredentials struct{ dsn string }

func (DBCredentials) MarshalJSON() ([]byte, error) { return nil, errPrivateEvidence }
func (DBCredentials) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[private DB credentials]")
}

func ReadDBCredentials(ctx context.Context, file *os.File) (*DBCredentials, error) {
	b, err := readCredentialPipe(ctx, file)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	var input struct {
		DSN string `json:"dsn"`
	}
	if !exactPrivateObject(b, []string{"dsn"}, false) || uniqueJSON(json.NewDecoder(bytes.NewReader(b)), 0) != nil {
		return nil, errCredentials
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF {
		return nil, errCredentials
	}
	c := &DBCredentials{dsn: input.DSN}
	if _, err := c.config(); err != nil {
		return nil, errCredentials
	}
	return c, nil
}

func (c *DBCredentials) config() (*pgxpool.Config, error) {
	if c == nil || len(c.dsn) > 4096 {
		return nil, errCredentials
	}
	// pgx merges PG* environment and service/pass files even with an explicit
	// URL. Refuse that discovery instead of silently using trading credentials.
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "PG") && value != "" {
			return nil, errCredentials
		}
	}
	u, err := url.Parse(c.dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil || u.User.Username() == "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errCredentials
	}
	password, hasPassword := u.User.Password()
	host := u.Hostname()
	port, portErr := strconv.Atoi(u.Port())
	if !hasPassword || password == "" || host == "" || strings.ContainsAny(host, ",/\\ \t\n") || portErr != nil || port < 1 || port > 65535 || !strings.HasPrefix(u.Path, "/") || len(u.Path) < 2 || strings.Contains(u.Path[1:], "/") {
		return nil, errCredentials
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["sslmode"]) != 1 {
		return nil, errCredentials
	}
	mode := q.Get("sslmode")
	if mode != "verify-full" && !(mode == "disable" && (host == "127.0.0.1" || host == "::1")) {
		return nil, errCredentials
	}
	// Disable the driver's implicit client certificate/passfile lookup, then
	// install verified TLS explicitly. System CAs only; no fallback plaintext.
	u.RawQuery = url.Values{"sslmode": {"disable"}, "passfile": {"/dev/null"}, "connect_timeout": {"5"}}.Encode()
	cfg, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return nil, errCredentials
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.Fallbacks = nil
	cfg.ConnConfig.RuntimeParams = map[string]string{"application_name": "research-collect", "default_transaction_read_only": "on"}
	if mode == "verify-full" {
		cfg.ConnConfig.TLSConfig = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	return cfg, nil
}

func (c *DBCredentials) OpenPool(ctx context.Context) (*pgxpool.Pool, error) {
	cfg, err := c.config()
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errDB
	}
	return pool, nil
}
