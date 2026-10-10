// Package collect validates research evidence independently of live trading.
package collect

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"trade_bot/internal/models"
)

var errSnapshot = errors.New("invalid_research_snapshot")

// DecodeSnapshot verifies the producer's canonical checksum after JSONB key
// reordering. A complete capture is NOT proof of complete exchange history.
// Neither input text nor decode errors cross this boundary.
func DecodeSnapshot(raw []byte, protocol string) (models.ResearchCaptureInput, string, error) {
	fail := func() (models.ResearchCaptureInput, string, error) {
		return models.ResearchCaptureInput{}, "", errSnapshot
	}
	if len(raw) == 0 || len(raw) > models.ResearchSnapshotMaxBytes {
		return fail()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSON(d, 0); err != nil {
		return fail()
	}
	if _, err := d.Token(); err != io.EOF {
		return fail()
	}
	var envelope struct {
		Payload struct {
			Schema        int      `json:"schema"`
			CaptureStatus string   `json:"capture_status"`
			Reasons       []string `json:"reasons"`
			models.ResearchCaptureInput
		} `json:"payload"`
		Checksum string `json:"checksum"`
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&envelope) != nil || envelope.Payload.Schema != 1 || protocol == "" || envelope.Payload.ProtocolID != protocol {
		return fail()
	}
	canonical, code := models.BuildResearchEntrySnapshot(envelope.Payload.ResearchCaptureInput)
	if code != "" {
		return fail()
	}
	// Exact field presence matters: missing false/zero/null is not the same as
	// observed false/zero/null. Comparing the full canonical envelope also checks
	// status/reasons and checksum, without treating JSONB's key order as evidence.
	var actual, expected any
	decode := func(data []byte, target *any) error {
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		return d.Decode(target)
	}
	if decode(raw, &actual) != nil || decode(canonical, &expected) != nil || !sameJSON(actual, expected) {
		return fail()
	}
	return envelope.Payload.ResearchCaptureInput, envelope.Payload.CaptureStatus, nil
}

// JSONB normalizes decimal notation, not precision. Never round the input to
// float64 for comparison: 2.00000000000000001 is not the producer's 2.
func sameJSON(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		left, lok := jsonRational(x)
		right, rok := jsonRational(y)
		return lok && rok && left.Cmp(right) == 0
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			other, exists := y[k]
			if !exists || !sameJSON(v, other) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

func jsonRational(n json.Number) (*big.Rat, bool) {
	raw := n.String()
	// Covers all producer float64/int values, including subnormal JSONB decimal
	// expansion, while preventing hostile exponents from allocating huge ints.
	if len(raw) > 1024 {
		return nil, false
	}
	if index := strings.IndexAny(raw, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(raw[index+1:])
		if err != nil || exponent < -400 || exponent > 400 {
			return nil, false
		}
	}
	return new(big.Rat).SetString(raw)
}

func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errSnapshot
	}
	token, err := d.Token()
	if err != nil {
		return errSnapshot
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return errSnapshot
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errSnapshot
			}
			seen[name] = true
			if err := uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errSnapshot
	}
	_, err = d.Token()
	return err
}
