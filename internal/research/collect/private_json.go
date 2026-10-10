//go:build linux || darwin

package collect

import (
	"encoding/json"
	"strings"
)

// encoding/json matches struct tags case-insensitively. Validate wire names
// first, so aliases cannot override known fields. Exact duplicate keys are
// rejected separately by uniqueJSON at the bounded input/HTTP boundary.
func exactPrivateObject(body []byte, fields []string, allowUnknown bool) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || object == nil {
		return false
	}
	for key := range object {
		known := false
		for _, field := range fields {
			if key == field {
				known = true
				break
			}
			if strings.EqualFold(key, field) {
				return false
			}
		}
		if !known && !allowUnknown {
			return false
		}
	}
	return true
}

func exactPrivateResponse(body []byte, rowFields []string) bool {
	if !exactPrivateObject(body, []string{"code", "data"}, true) {
		return false
	}
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data == nil {
		return false
	}
	for _, row := range envelope.Data {
		if !exactPrivateObject(row, rowFields, true) {
			return false
		}
	}
	return true
}
