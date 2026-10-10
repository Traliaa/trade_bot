//go:build linux || darwin

package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"
)

// PrivateCredentials is intentionally opaque. It never discovers keys in env,
// files or the trading database. Even a read-only credential must not be logged.
type PrivateCredentials struct{ key, secret, passphrase, expectedUID string }

func (PrivateCredentials) MarshalJSON() ([]byte, error) { return nil, errPrivateEvidence }
func (PrivateCredentials) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[private credentials]")
}

var errCredentials = errors.New("research_credentials_invalid")
var privateID = regexp.MustCompile(`^[1-9][0-9]{0,127}$`)

// ReadPrivateCredentials reads an explicitly inherited pipe, not a filename.
// The caller owns the FD and must not read it concurrently. The sender must close
// its end. Cancel/deadline stops the read without a stranded reader goroutine.
func ReadPrivateCredentials(ctx context.Context, file *os.File) (*PrivateCredentials, error) {
	b, err := readCredentialPipe(ctx, file)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	return decodePrivateCredentials(b)
}

func readCredentialPipe(ctx context.Context, file *os.File) ([]byte, error) {
	if file == nil || ctx.Err() != nil {
		return nil, errCredentials
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return nil, errCredentials
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if file.SetReadDeadline(deadline) != nil {
		return nil, errCredentials
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = file.SetReadDeadline(time.Now()); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
		_ = file.SetReadDeadline(time.Time{})
	}()
	b, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || ctx.Err() != nil || len(b) > 4096 || len(b) == 0 {
		clear(b)
		return nil, errCredentials
	}
	return b, nil
}

func decodePrivateCredentials(b []byte) (*PrivateCredentials, error) {
	if uniqueJSON(json.NewDecoder(bytes.NewReader(b)), 0) != nil {
		return nil, errCredentials
	}
	if !exactPrivateObject(b, []string{"api_key", "secret_key", "passphrase", "expected_uid"}, false) {
		return nil, errCredentials
	}
	var input struct {
		Key        string `json:"api_key"`
		Secret     string `json:"secret_key"`
		Passphrase string `json:"passphrase"`
		UID        string `json:"expected_uid"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF {
		return nil, errCredentials
	}
	c := &PrivateCredentials{input.Key, input.Secret, input.Passphrase, input.UID}
	if !c.valid() {
		return nil, errCredentials
	}
	return c, nil
}
func (c *PrivateCredentials) valid() bool {
	if c == nil || !privateID.MatchString(c.expectedUID) {
		return false
	}
	for _, v := range []string{c.key, c.secret, c.passphrase} {
		if len(v) == 0 || len(v) > 256 {
			return false
		}
		for _, r := range v {
			if r < 33 || r > 126 {
				return false
			}
		}
	}
	return true
}
